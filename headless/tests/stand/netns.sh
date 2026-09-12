set -u

NETNS_CREATED=""

setup_ns() {
    _name="$1"
    _sub="$2"
    _gw="10.201.$_sub.1"
    _addr="10.201.$_sub.2"
    ip netns add "$_name"
    ip link add "veth-$_name" type veth peer name "peer-$_name"
    ip link set "peer-$_name" netns "$_name"
    ip addr add "$_gw/30" dev "veth-$_name"
    ip link set "veth-$_name" up
    ip netns exec "$_name" ip link set "peer-$_name" name eth0
    ip netns exec "$_name" ip link set lo up
    ip netns exec "$_name" ip addr add "$_addr/30" dev eth0
    ip netns exec "$_name" ip link set eth0 up
    ip netns exec "$_name" ip route add default via "$_gw"
    iptables -t nat -A POSTROUTING -s "$_addr/32" -j MASQUERADE
    mkdir -p "/etc/netns/$_name"
    _ns=$(grep '^nameserver' /etc/resolv.conf | grep -v ' 127\.' || true)
    [ -n "$_ns" ] || _ns="nameserver 1.1.1.1"
    printf '%s\n' "$_ns" >"/etc/netns/$_name/resolv.conf"
    NETNS_CREATED="$NETNS_CREATED $_name:$_addr"
}

teardown_ns() {
    for _entry in $NETNS_CREATED; do
        _name="${_entry%%:*}"
        _addr="${_entry#*:}"
        iptables -t nat -D POSTROUTING -s "$_addr/32" -j MASQUERADE 2>/dev/null
        ip netns delete "$_name" 2>/dev/null
        rm -rf "/etc/netns/$_name"
    done
    NETNS_CREATED=""
}
