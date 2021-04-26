#! /bin/bash

set -x 
set -e


cat <<'EOF' > /lib/systemd/system/uproxy.service
[Unit]
Description=URP gRPC Proxy
After=network.target

[Service]
Restart=always
ExecStart=/usr/local/sbin/uproxy-runner
 
[Install]
WantedBy=multi-user.target
EOF

cat <<'EOF' > /usr/local/sbin/uproxy-runner
#! /bin/bash

set -x 
set -e

mkdir -p /etc/uproxy/
# wget # TODO github releases

chmod 555 /etc/uproxy/uproxy
chown root.root /etc/uproxy/uproxy

cat <<'IOF' > /etc/uproxy/config.txt
# put config here
port = 8080
debugLevel = network
# network, state, always, or none
profListen = localhost:6066
# comments
# debug levels are (from low to high: network, state, all, none)
debugLevel = network
IOF

cd /etc/uproxy
ulimit -n 65535
nohup ./uproxy
EOF

chown root.root /usr/local/sbin/uproxy-runner
chmod 555 /usr/local/sbin/uproxy-runner

systemctl daemon-reload
systemctl enable uproxy
systemctl start uproxy

