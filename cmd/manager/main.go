package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	pb "github.com/OpenFogStack/tinyFaaS/mistify/registry/node"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/OpenFogStack/tinyFaaS/pkg/docker"
	"github.com/OpenFogStack/tinyFaaS/pkg/manager"
	"github.com/OpenFogStack/tinyFaaS/pkg/tfconfig"
	"github.com/pelletier/go-toml/v2"
)

type server struct {
	ms      *manager.ManagementService
	mistify pb.MistifyClient
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

	// connect to mistify
	log.Println("connecting to mistify")
	conn, err := grpc.Dial(
		fmt.Sprintf("localhost:%d", config.RegistryPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatal(err)
	}

	s.mistify = pb.NewMistifyClient(conn)

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
		FunctionName    string   `json:"name"`
		FunctionEnv     string   `json:"env"`
		FunctionThreads int      `json:"threads"`
		FunctionZip     string   `json:"zip"`
		FunctionEnvs    []string `json:"envs"`
	}{}

	err := json.NewDecoder(r.Body).Decode(&d)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println(err)
		return
	}

	if r.Header.Get("X-mistify-bypass") == "" {
		json, err := json.Marshal(d)
		if err != nil {
			log.Printf("failed to marshal function: %v", err)
			return
		}

		// register function with registry

		log.Println("registering function with mistify")

		_, err = s.mistify.RegisterFunction(context.Background(), &pb.Function{
			Name: d.FunctionName,
			Json: string(json),
		})

		if err != nil {
			log.Printf("failed to register function with mistify: %v", err)
		}

		return
	}

	log.Println("got request to upload function: Name", d.FunctionName, "Env", d.FunctionEnv, "Threads", d.FunctionThreads, "Bytes", len(d.FunctionZip), "Envs", d.FunctionEnvs)

	envs := make(map[string]string)
	for _, e := range d.FunctionEnvs {
		k, v, ok := strings.Cut(e, "=")

		if !ok {
			log.Println("invalid env:", e)
			continue
		}

		envs[k] = v
	}

	res, err := s.ms.Upload(d.FunctionName, d.FunctionEnv, d.FunctionThreads, d.FunctionZip, envs)

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
	var logs io.Reader
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
	// w.Header().Set("Content-Type", "text/plain")
	_, err := io.Copy(w, logs)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println(err)
		return
	}
}

func (s *server) urlUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// parse request
	d := struct {
		FunctionName    string   `json:"name"`
		FunctionEnv     string   `json:"env"`
		FunctionThreads int      `json:"threads"`
		FunctionURL     string   `json:"url"`
		FunctionEnvs    []string `json:"envs"`
		SubFolder       string   `json:"subfolder_path"`
	}{}

	err := json.NewDecoder(r.Body).Decode(&d)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println(err)
		return
	}

	log.Println("got request to upload function:", d)

	envs := make(map[string]string)
	for _, e := range d.FunctionEnvs {
		k, v, ok := strings.Cut(e, "=")

		if !ok {
			log.Println("invalid env:", e)
			continue
		}

		envs[k] = v
	}

	res, err := s.ms.UrlUpload(d.FunctionName, d.FunctionEnv, d.FunctionThreads, d.FunctionURL, d.SubFolder, envs)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println(err)
		return
	}

	// return success
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, res)
}
