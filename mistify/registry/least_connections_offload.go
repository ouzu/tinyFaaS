package registry

import (
	"math/rand"

	"github.com/charmbracelet/log"
)

type LeastConnectionsOffloadStrategy struct{}

func (l *LeastConnectionsOffloadStrategy) SelectNode(ctx *StrategyContext, name string) (*NodeSelectionResult, error) {
	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()

	log.Debugf("selecting node for function %s", name)

	// get the least amount of active requests
	min := ctx.ActiveRequests[ctx.Siblings[0].Address.Name]
	for _, node := range ctx.Siblings {
		if ctx.ActiveRequests[node.Address.Name] < min {
			min = ctx.ActiveRequests[node.Address.Name]
		}
	}

	// choose one among the siblings with the least number of active requests
	var minNodes []NodeConnection
	for _, node := range ctx.Siblings {
		if ctx.ActiveRequests[node.Address.Name] == min {
			minNodes = append(minNodes, node)
		}
	}

	target := minNodes[rand.Intn(len(minNodes))]

	log.Debugf("selecting node %s for function %s", target.Address.Name, name)

	result := &NodeSelectionResult{
		SelectedNode:     &target,
		NeedsDeployment:  true,
		DeploymentTarget: &target,
		SyncDeployment:   false,
	}

	// check if function is deployed on the target node
	if funcs, ok := ctx.DeployedFuncs[target.Address.Name]; ok {
		log.Debugf("target node has the following functions deployed: %v", funcs)
		for _, f := range funcs {
			if f == name {
				log.Debugf("function %s is deployed on the target node", name)
				result.NeedsDeployment = false
				break
			}
		}
	}

	if result.NeedsDeployment {
		result.SelectedNode = ctx.ParentNode
	}

	return result, nil
}
