package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"

	"github.com/OpenFogStack/tinyFaaS/pkg/docker"
	"github.com/OpenFogStack/tinyFaaS/pkg/manager"
	"github.com/OpenFogStack/tinyFaaS/pkg/tfconfig"
	"github.com/pelletier/go-toml/v2"
)

type server struct {
	ms *manager.ManagementService
	//rs *registry.RegistryService
}

func main() {
	config := tfconfig.DefaultConfig()

	if len(os.Args) == 2 {
		configFile := os.Args[1]
		log.Println("reading config from", configFile)

		// Read the config file into config variable
		file, err := os.ReadFile(configFile)
		if err != nil {
			log.Fatalf("Error reading the config file: %v", err)
		}

		if err := toml.Unmarshal(file, &config); err != nil {
			log.Fatalf("Error unmarshalling the config file: %v", err)
		}
	} else {
		log.Fatalln("Usage: ./manager <config-file>")
	}

	log.Printf("config: %+v\n", config)

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetPrefix("manager: ")

	var tfBackend manager.Backend

	switch config.Backend {
	case "docker":
		log.Println("using docker backend")
		tfBackend = docker.New(config.ID)
	default:
		log.Fatalf("invalid backend %s", config.Backend)
	}

	/*
		var rgs registry.RegistryService

		switch config.Mode {
		case "edge":
			log.Println("running in edge mode")
			rgs = registry.NewEdgeNode(config.ID, config.Address, config.RegistryPort, config.ParentAddress)
		case "fog":
			log.Println("running in fog mode")
			rgs = registry.NewFogNode(config.ID, config.Address, config.RegistryPort, config.ParentAddress)
		case "cloud":
			log.Println("running in cloud mode")
			rgs = registry.NewRootNode(config.ID, config.Address, config.RegistryPort)
		default:
			log.Fatalf("invalid mode %s", config.Mode)
		}
	*/

	ports := map[string]int{
		"coap": config.COAPPort,
		"http": config.HTTPPort,
		"grpc": config.GRPCPort,
	}

	ms := manager.New(
		config.ID,
		config.RProxyListenAddress,
		ports,
		config.RProxyConfigPort,
		tfBackend,
	)

	/*
		rproxyArgs := []string{fmt.Sprintf("%s:%d", config.RProxyListenAddress, config.RProxyConfigPort)}

		for prot, port := range ports {
			rproxyArgs = append(rproxyArgs, fmt.Sprintf("%s:%s:%d", prot, config.RProxyListenAddress, port))
		}
	*/

	rproxyArgs := []string{os.Args[1]}

	log.Println("rproxy args:", rproxyArgs)
	c := exec.Command(config.RProxyBin, rproxyArgs...)

	stdout, err := c.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}

	stderr, err := c.StderrPipe()
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
	}()

	err = c.Start()
	if err != nil {
		log.Fatal(err)
	}

	rproxy := c.Process

	log.Println("started rproxy")

	s := &server{
		ms: ms,
		//rs: &rgs,
	}

	// create handlers
	r := http.NewServeMux()
	r.HandleFunc("/upload", s.uploadHandler)
	r.HandleFunc("/delete", s.deleteHandler)
	r.HandleFunc("/list", s.listHandler)
	r.HandleFunc("/wipe", s.wipeHandler)
	r.HandleFunc("/logs", s.logsHandler)
	r.HandleFunc("/uploadURL", s.urlUploadHandler)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig

		log.Println("received interrupt")
		log.Println("shutting down")

		// stop rproxy
		log.Println("stopping rproxy")
		err := rproxy.Kill()

		if err != nil {
			log.Println(err)
		}

		// stop handlers
		log.Println("stopping management service")
		err = ms.Stop()

		if err != nil {
			log.Println(err)
		}

		os.Exit(0)
	}()

	// start registry service
	// log.Println("starting registry service")
	// go rgs.Start()

	// start server
	log.Println("starting HTTP server")
	addr := fmt.Sprintf(":%d", config.ConfigPort)
	err = http.ListenAndServe(addr, r)
	if err != nil {
		log.Fatal(err)
	}
}

func (s *server) uploadHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// parse request
	d := struct {
		FunctionName    string `json:"name"`
		FunctionEnv     string `json:"env"`
		FunctionThreads int    `json:"threads"`
		FunctionZip     string `json:"zip"`
	}{}

	err := json.NewDecoder(r.Body).Decode(&d)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println(err)
		return
	}

	log.Println("got request to upload function: Name", d.FunctionName, "Env", d.FunctionEnv, "Threads", d.FunctionThreads, "Bytes", len(d.FunctionZip))

	res, err := s.ms.Upload(d.FunctionName, d.FunctionEnv, d.FunctionThreads, d.FunctionZip)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println(err)
		return
	}

	// return success
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, res)

}

func (s *server) deleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// parse request
	d := struct {
		FunctionName string `json:"name"`
	}{}

	err := json.NewDecoder(r.Body).Decode(&d)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println(err)
		return
	}

	log.Println("got request to delete function:", d.FunctionName)

	// delete function
	err = s.ms.Delete(d.FunctionName)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println(err)
		return
	}

	// return success
	w.WriteHeader(http.StatusOK)
}

func (s *server) listHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	l := s.ms.List()

	// return success
	w.WriteHeader(http.StatusOK)
	for _, f := range l {
		fmt.Fprintf(w, "%s\n", f)
	}
}

func (s *server) wipeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err := s.ms.Wipe()

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println(err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *server) logsHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// parse request
	var logs string
	name := r.URL.Query().Get("name")

	if name == "" {
		l, err := s.ms.Logs()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Println(err)
			return
		}
		logs = l
	}

	if name != "" {
		l, err := s.ms.LogsFunction(name)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Println(err)
			return
		}
		logs = l
	}

	// return success
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, logs)
}

func (s *server) urlUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// parse request
	d := struct {
		FunctionName    string `json:"name"`
		FunctionEnv     string `json:"env"`
		FunctionThreads int    `json:"threads"`
		FunctionURL     string `json:"url"`
		SubFolder       string `json:"subfolder_path"`
	}{}

	err := json.NewDecoder(r.Body).Decode(&d)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println(err)
		return
	}

	log.Println("got request to upload function:", d)

	res, err := s.ms.UrlUpload(d.FunctionName, d.FunctionEnv, d.FunctionThreads, d.FunctionURL, d.SubFolder)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println(err)
		return
	}

	// return success
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, res)
}
