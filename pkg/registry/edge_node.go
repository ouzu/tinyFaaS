package registry

import (
	"context"
	"fmt"

	pb "github.com/OpenFogStack/tinyFaaS/pkg/registry/node"
)

type EdgeNode struct {
	BaseNode
}

func NewEdgeNode(name string, address string, port int, parent string) *EdgeNode {
	return &EdgeNode{
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

func (e *EdgeNode) Register(ctx context.Context, in *pb.NodeAddress) (*pb.Empty, error) {
	return nil, fmt.Errorf("edge nodes do not accept registrations")
}
