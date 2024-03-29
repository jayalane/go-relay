Transparent Proxy for non-HTTP things out via a squid proxy
===========================================================

This code will probably be mostly useful to people using AWS who have
pretty restrictive outbound internet access.  It is designed to be run
on a machine that is the default route for a private VPC; the box
needs nat on it, something like:

```
    iptables -t nat -A PREROUTING -p tcp --dport 22 --to-port 6000
```

It reads the address from Host: line, SNI in the TLS handshake, or
lastely, the original destination in the pre-NAT local address.  SNI
and Host are preferred as our squid config is DNS based.  

Went with just 4 go-routines per connection-pair, one each to read
from each connection and one each to write to each connection.  I
don't think this will be very high volume, probably few thousand
connections max.  (Tho docker pulls do crank up the size of each
flow.)

Commit 11de0c189812257a860e3bffce614148388b74a9 is first that will runs under 
load for a pretty long time correctly.  Previous versions worked but had
cass when they didn't close connections.  

Commit 60ab102b50a3a441f5fdf548c088ebf67e6ac053 is pretty good, much
less memory usage.  Still something not totally happy with Cloudera
cluster managers.

Commit 50232e28f15d5685324dda31319de7f49998f2a5 fixes an FD leak
(blocking in the channel for writes blocks forever when the connection
is torn down; added a writesDone channel that is also selected for and
causes writing go-routines to exit.  Also per-endpoint stats.  And a
regression on infinite loops on close write fd.

Starting with commit 2cac348a5cf5f2b11a3cba98ecdcbaf66c46f0fd and
until 20b7fe05ec2ed8bd4e0f0692d3569d882a3f6e11 there was a regression.
Working good now.  HUP reloads logging config, tested.

For UDP, it expects to talk to a squid proxy and CONNECT to a copy of
./server that is an gRPC server proxying UDP.  The state tracking
of sender/receiver pairs is all in the RPC server, the transproxy is
stateless.  

For a while the UDP proxy broke TCP functionality, but with this
commit 6c254434aa1261f2cfa57d1845e0327aecbe3401 they both work (I had
an earlier commit ID in here earlier but I think I was confused about
the state of my routing and it didn't work).  Also the misc directory
has some scripts that make systemd scripts to install and run them.

WIth 484a059cdcf4997a09a2a56e87ed73fa8be4a72d UDP proxying works well
enough for my laptop to do DNS queries thru it.  
