package registry

import pb "github.com/OpenFogStack/tinyFaaS/pkg/registry/node"

type FogNode struct {
	BaseNode
}

func NewFogNode(name string, address string, port int, parent string) *FogNode {
	return &FogNode{
		BaseNode: BaseNode{
			port: port,
			self: pb.NodeAddress{
				Address: address,
				Name:    name,
			},
			parentAddr: parent,
		},
	}
}
