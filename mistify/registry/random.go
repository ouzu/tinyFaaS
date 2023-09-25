package registry

import (
	"math/rand"

	"github.com/charmbracelet/log"
)

type RandomStrategy struct{}

func (l *RandomStrategy) SelectNode(ctx *StrategyContext, name string) (*NodeSelectionResult, error) {
	// Select a random node
	target := ctx.Siblings[rand.Intn(len(ctx.Siblings))]

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
