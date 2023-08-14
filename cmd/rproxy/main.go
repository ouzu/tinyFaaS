package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/OpenFogStack/tinyFaaS/pkg/coap"
	"github.com/OpenFogStack/tinyFaaS/pkg/grpc"
	tfhttp "github.com/OpenFogStack/tinyFaaS/pkg/http"
	"github.com/OpenFogStack/tinyFaaS/pkg/rproxy"
	"github.com/OpenFogStack/tinyFaaS/pkg/tfconfig"
	"github.com/pelletier/go-toml/v2"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetPrefix("rproxy: ")

	if len(os.Args) != 2 {
		log.Fatalln("Usage: ./rproxy <config-file>")
	}

	configFile := os.Args[1]
	log.Println("reading config from", configFile)

	// Read the config file into config variable
	file, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Error reading the config file: %v", err)
	}

	config := tfconfig.DefaultConfig()
	if err := toml.Unmarshal(file, &config); err != nil {
		log.Fatalf("Error unmarshalling the config file: %v", err)
	}

	r := rproxy.New()

	// CoAP
	coapAddr := fmt.Sprintf("%s:%d", config.RProxyListenAddress, config.COAPPort)
	log.Printf("adding coap listener on %s", coapAddr)
	go coap.Start(r, coapAddr)

	// HTTP
	httpAddr := fmt.Sprintf("%s:%d", config.RProxyListenAddress, config.HTTPPort)
	log.Printf("adding http listener on %s", httpAddr)
	go tfhttp.Start(r, httpAddr)

	// GRPC
	grpcAddr := fmt.Sprintf("%s:%d", config.RProxyListenAddress, config.GRPCPort)
	log.Printf("adding grpc listener on %s", grpcAddr)
	go grpc.Start(r, grpcAddr)

	server := http.NewServeMux()

	server.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		log.Printf("have request: %+v", req)

		buf := new(bytes.Buffer)
		buf.ReadFrom(req.Body)
		newStr := buf.String()

		log.Printf("have body: %s", newStr)

		var def struct {
			FunctionResource   string   `json:"name"`
			FunctionContainers []string `json:"ips"`
		}

		err := json.Unmarshal([]byte(newStr), &def)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		log.Printf("have definition: %+v", def)

		if def.FunctionResource[0] == '/' {
			def.FunctionResource = def.FunctionResource[1:]
		}

		if len(def.FunctionContainers) > 0 {
			// "ips" field not empty: add function
			log.Printf("adding %s", def.FunctionResource)
			err = r.Add(def.FunctionResource, def.FunctionContainers)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
			return
		} else {

			log.Printf("deleting %s", def.FunctionResource)
			err = r.Del(def.FunctionResource)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return

			}
		}
	})

	rproxyAddr := fmt.Sprintf("%s:%d", config.RProxyListenAddress, config.RProxyConfigPort)

	log.Printf("listening on %s", rproxyAddr)
	err = http.ListenAndServe(rproxyAddr, server)

	if err != nil {
		log.Printf("%s", err)
	}

	log.Printf("exiting")
}
