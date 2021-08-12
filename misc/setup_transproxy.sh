#! /bin/bash

set -x 
set -e


cat <<'EOF' > /lib/systemd/system/transproxy.service
[Unit]
Description=Transparent Proxy
After=network.target

[Service]
Restart=always
RestartSec=5s
ExecStart=/usr/local/sbin/transproxy-runner
 
[Install]
WantedBy=multi-user.target
EOF

cat <<'EOF' > /usr/local/sbin/transproxy-runner
#! /bin/bash

set -x 
set -e

# todo get iptables 

mkdir -p /etc/transproxy/
# wget # TODO github releases

iptables -F
iptables -t nat -F 
iptables -t mangle -F
iptables -X
iptables -P INPUT ACCEPT
iptables -P FORWARD ACCEPT
iptables -P OUTPUT ACCEPT

iptables -t mangle -A PREROUTING -p udp --dport 53 -j MARK --set-mark 1
ip rule delete table 100 || echo "No rule already"
ip rule add fwmark 1 lookup 100
ip route add local 0.0.0.0/0 dev lo table 100


for p in 22 80 443  ; do
    iptables -t nat -A PREROUTING -p tcp --dport $p -j REDIRECT --to-port 5999
done

chmod 555 /etc/transproxy/transproxy
chown root.root /etc/transproxy/transproxy

cat <<'IOF' > /etc/transproxy/config.txt
# put config here
ports = 5999
udpPorts = 53
destHostOverride = 
destPortOverride = 
numConnectionHandlers = 20
squidHost = localhost
squidPort = 4128
squidForSNI = true
profListen = localhost:6060
#destCidrUseSquid = 45.33.0.0/16
destCidrUseSquid = 0.0.0.0/0
#destCidrUseSquid = 127.1.1.1/32
srcCidrBan = 129.0.0.0/8
# debug levels are (from low to high: network, state, all, none)
debugLevel = network
# comments
IOF

cd /etc/transproxy
ulimit -n 65535
nohup ./transproxy
EOF

chown root.root /usr/local/sbin/transproxy-runner
chmod 555 /usr/local/sbin/transproxy-runner

systemctl daemon-reload
systemctl enable transproxy
systemctl start transproxy

