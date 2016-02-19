package main

import (
	_ "github.com/xytis/registrator/consul"
	_ "github.com/xytis/registrator/consulkv"
	_ "github.com/xytis/registrator/etcd"
	_ "github.com/xytis/registrator/skydns2"
	_ "github.com/xytis/registrator/zookeeper"
)
