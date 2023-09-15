package main

import (
	"os"
	"strings"
	"time"

	"github.com/OpenFogStack/tinyFaaS/mistify/registry"
	"github.com/OpenFogStack/tinyFaaS/pkg/tfconfig"
	"github.com/charmbracelet/log"
	"github.com/pelletier/go-toml/v2"
)

func main() {
	config := tfconfig.DefaultConfig()

	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		log.SetLevel(log.DebugLevel)
		log.SetReportCaller(true)
	default:
		log.SetLevel(log.InfoLevel)
	}

	log.SetTimeFormat(time.Stamp)

	if len(os.Args) == 2 {
		configFile := os.Args[1]
		log.Debugf("reading config from %s", configFile)

		// Read the config file into config variable
		file, err := os.ReadFile(configFile)
		if err != nil {
			log.Fatalf("Error reading the config file: %v", err)
		}

		if err := toml.Unmarshal(file, &config); err != nil {
			log.Fatalf("Error unmarshalling the config file: %v", err)
		}
	} else {
		log.Fatal("Usage: ./manager <config-file>")
	}

	log.SetPrefix(config.ID)

	log.Debugf("config: %+v\n", config)

	var rgs registry.RegistryService

	switch config.Mode {
	case "cloud":
		log.Debug("running in cloud mode")
		rgs = registry.NewRootNode(&config)
	case "fog":
		log.Debug("running in fog mode")
		rgs = registry.NewFogNode(&config)
	case "edge":
		log.Debug("running in edge mode")
		rgs = registry.NewEdgeNode(&config)
	default:
		log.Fatalf("invalid mode %s", config.Mode)
	}

	rgs.Start()
}
