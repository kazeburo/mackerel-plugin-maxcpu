# mackerel-plugin-maxcpu

Mackerel plugin for Calculating Max/Min/Average CPU Usage in Period 

## usage

```
Usage:
  mackerel-plugin-maxcpu [OPTIONS]

Application Options:
  -s, --socket=    Socket file used calcurating daemon
      --as-daemon  run as daemon
  -v, --version    Show version

Help Options:
  -h, --help       Show this help message
```

At the first time of execution, mackerel-plugin-maxcpu spawns the calculating daemon. From second execution mackerel-plugin-maxcpu connects the daemon to know CPU usages.

## sample

```
$  ./mackerel-plugin-maxcpu --socket /tmp/maxcpu.sock
2020/10/29 15:29:47 Get "http://unix/hc": dial unix /tmp/maxcpu.sock: connect: no such file or directory
2020/10/29 15:29:47 start background process
$  ./mackerel-plugin-maxcpu --socket /tmp/maxcpu.sock
maxcpu.user_sys_iowa_softi_usage.max    0.748130        1603952991
maxcpu.user_sys_iowa_softi_usage.min    0.000000        1603952991
maxcpu.user_sys_iowa_softi_usage.avg    0.333340        1603952991
maxcpu.user_sys_iowa_softi_usage.90pt   0.748130        1603952991
maxcpu.user_sys_iowa_softi_usage.75pt   0.251889        1603952991
```
