Transparent Proxy for non-HTTP things out via a squid proxy
===========================================================

This code will probably be mostly useful to people using AWS who have
pretty restrictive outbound internet access.  It is designed to be run
on a machine that is the default route for a private VPC; the box
needs nat on it, something like:

```
    iptables -t nat -A PREROUTING -p tcp --dport 22 --to-port 6000
```

This currently is just hard coded to us my squid on my laptop to ssh
to my Linode.  Once I get a way to map the real IPs I will be getting
to hostnames (that my squid config allows), I'll add some mapping
between the local address on the socket, which I think will be the out
on the internet address of the real destination and host names (that
can be used in the CONNECT line and Host: line of the squid
pre-amble).  If squid allows the CONNECT, then it just glues the bytes
together.

Went with just 4 go-routines per connection-pair, one each to read
from each connection and one each to write to each connection.  I
don't think this will be very high volume, probably few thousand
connections max.  

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
Working good now.  Haven't tested HUP re-config outside of laptop yet.