package registry

import (
	"math/rand"

	"github.com/charmbracelet/log"
)

type RandomHotStrategy struct{}

func (l *RandomHotStrategy) SelectNode(ctx *StrategyContext, name string) (*NodeSelectionResult, error) {
	// build a list of nodes that have the function deployed
	var hotNodes []NodeConnection
	for _, node := range ctx.Siblings {
		if funcs, ok := ctx.DeployedFuncs[node.Address.Name]; ok {
			for _, f := range funcs {
				if f == name {
					hotNodes = append(hotNodes, node)
					break
				}
			}
		}
	}

	// check if any nodes have the function deployed
	if len(hotNodes) == 0 {
		log.Infof("no sibling nodes have function %s deployed, escalating to parent", name)

		// choose a random sibling to deploy the function
		target := ctx.Siblings[rand.Intn(len(ctx.Siblings))]
		log.Debugf("selecting node %s for deployment of %s", target.Address.Name, name)

		return &NodeSelectionResult{
			SelectedNode:     ctx.ParentNode,
			NeedsDeployment:  true,
			DeploymentTarget: &target,
			SyncDeployment:   false,
		}, nil
	}

	// Select a random node
	target := hotNodes[rand.Intn(len(hotNodes))]

	log.Debugf("selecting node %s for function %s", target.Address.Name, name)

	return &NodeSelectionResult{
		SelectedNode: &target,
	}, nil
}
