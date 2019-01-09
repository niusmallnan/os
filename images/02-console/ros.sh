if [ -d /var/lib/rancher/profile.d ]; then
  for i in /var/lib/rancher/profile.d/*.sh; do
    if [ -r $i ]; then
      . $i
    fi
  done
  unset i
fi

export LD_LIBRARY_PATH=/usr/lib
