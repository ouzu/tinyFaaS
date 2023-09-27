package registry

import (
	"math/rand"

	"github.com/charmbracelet/log"
)

type RoundRobinHotStrategy struct {
	n int
}

func (l *RoundRobinHotStrategy) SelectNode(ctx *StrategyContext, name string) (*NodeSelectionResult, error) {
	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()

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
	// TODO: check for threshold like in the least busy strategy
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

	// Select the next node
	l.n = (l.n + 1) % len(hotNodes)

	target := hotNodes[l.n]

	log.Debugf("selecting node %s for function %s", target.Address.Name, name)

	result := &NodeSelectionResult{
		SelectedNode:     &target,
		NeedsDeployment:  true,
		DeploymentTarget: &target,
		SyncDeployment:   true,
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

	return result, nil
}
