package sysinit

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/rancher/os/cmd/control"
	"github.com/rancher/os/config"
	"github.com/rancher/os/pkg/compose"
	"github.com/rancher/os/pkg/docker"
	"github.com/rancher/os/pkg/log"

	"github.com/docker/engine-api/types"
	"github.com/docker/libcompose/project/options"
	"golang.org/x/net/context"
)

const (
	systemImagesPreloadDirectory = "/var/lib/rancher/preload/system-docker"
	systemImagesLoadStamp        = "/var/lib/rancher/.sysimages_%s_loaded.done"
)

func hasImage(name string) bool {
	stamp := path.Join(config.StateDir, name)
	if _, err := os.Stat(stamp); os.IsNotExist(err) {
		return false
	}
	return true
}

func getImagesArchive(bootstrap bool) string {
	var archive string
	if bootstrap {
		archive = path.Join(config.ImagesPath, config.InitImages)
	} else {
		archive = path.Join(config.ImagesPath, config.SystemImages)
	}

	return archive
}

func LoadBootstrapImages(cfg *config.CloudConfig) (*config.CloudConfig, error) {
	return loadImages(cfg, true)
}

func LoadSystemImages(cfg *config.CloudConfig) (*config.CloudConfig, error) {
	stamp := fmt.Sprintf(systemImagesLoadStamp, strings.Replace(config.Version, ".", "_", -1))
	if _, err := os.Stat(stamp); os.IsNotExist(err) {
		os.Create(stamp)
		return loadImages(cfg, false)
	}

	log.Infof("Skipped loading system images because %s exists", systemImagesLoadStamp)
	return cfg, nil
}

func loadImages(cfg *config.CloudConfig, bootstrap bool) (*config.CloudConfig, error) {
	archive := getImagesArchive(bootstrap)

	client, err := docker.NewSystemClient()
	if err != nil {
		return cfg, err
	}

	if !hasImage(filepath.Base(archive)) {
		if _, err := os.Stat(archive); os.IsNotExist(err) {
			log.Fatalf("FATAL: Could not load images from %s (file not found)", archive)
		}

		// client.ImageLoad is an asynchronous operation
		// To ensure the order of execution, use cmd instead of it
		tarfile := fmt.Sprintf("/tmp/%s", strings.Replace(filepath.Base(archive), ".zst", "", -1))
		cmd := exec.Command("/usr/share/ros/zstd", "-d", "-o", tarfile, archive)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Errorf("Failed to run zstd: %v", err)
		}

		log.Infof("Loading images from %s", tarfile)
		cmd = exec.Command("/usr/bin/system-docker", "load", "-q", "-i", tarfile)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Fatalf("FATAL: Error loading images from %s (%v)\n%s ", tarfile, err, out)
		}

		log.Infof("Done loading images from %s", archive)
	}

	dockerImages, _ := client.ImageList(context.Background(), types.ImageListOptions{})
	for _, dimg := range dockerImages {
		log.Debugf("Loaded a docker image: %s", dimg.RepoTags)
	}

	return cfg, nil
}

func SysInit() error {
	cfg := config.LoadConfig()

	if err := control.PreloadImages(docker.NewSystemClient, systemImagesPreloadDirectory); err != nil {
		log.Errorf("Failed to preload System Docker images: %v", err)
	}

	_, err := config.ChainCfgFuncs(cfg,
		config.CfgFuncs{
			{"loadSystemImages", LoadSystemImages},
			{"start project", func(cfg *config.CloudConfig) (*config.CloudConfig, error) {
				p, err := compose.GetProject(cfg, false, true)
				if err != nil {
					return cfg, err
				}
				return cfg, p.Up(context.Background(), options.Up{
					Create: options.Create{
						NoRecreate: true,
					},
					Log: cfg.Rancher.Log,
				})
			}},
			{"sync", func(cfg *config.CloudConfig) (*config.CloudConfig, error) {
				syscall.Sync()
				return cfg, nil
			}},
			{"banner", func(cfg *config.CloudConfig) (*config.CloudConfig, error) {
				log.Infof("RancherOS %s started", config.Version)
				return cfg, nil
			}}})
	return err
}

func RunSysInit(c *config.CloudConfig) (*config.CloudConfig, error) {
	args := append([]string{config.SysInitBin}, os.Args[1:]...)

	cmd := &exec.Cmd{
		Path: config.RosBin,
		Args: args,
	}

	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if err := cmd.Start(); err != nil {
		return c, err
	}

	return c, os.Stdin.Close()
}
