package registry

import (
	"context"
	"fmt"

	pb "github.com/OpenFogStack/tinyFaaS/pkg/registry/node"
)

type RootNode struct {
	BaseNode
}

func NewRootNode(name string, address string, port int) *RootNode {
	return &RootNode{
		BaseNode: BaseNode{
			port: port,
			self: pb.NodeAddress{
				Address: address,
				Name:    name,
			},
		},
	}
}

func (r *RootNode) Start() {
	go r.serve()
}

func (r *RootNode) UpdateSiblingList(ctx context.Context, in *pb.SiblingList) (*pb.Empty, error) {
	return nil, fmt.Errorf("the root node has no siblings")
}
