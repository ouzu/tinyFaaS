package rproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sync"

	pb "github.com/OpenFogStack/tinyFaaS/mistify/registry/node"

	"github.com/OpenFogStack/tinyFaaS/pkg/tfconfig"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Status uint32

const (
	StatusOK Status = iota
	StatusAccepted
	StatusNotFound
	StatusError
)

type RProxy struct {
	hosts   map[string][]string
	hl      sync.RWMutex
	mistify pb.MistifyClient
}

func New(config *tfconfig.TFConfig) *RProxy {
	mistifyAddr := fmt.Sprintf("%s:%d", config.Host, config.RegistryPort)

	log.Printf("connecting to mistify at %s", mistifyAddr)

	conn, err := grpc.Dial(
		mistifyAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("failed to dial: %v", err)
	}

	return &RProxy{
		hosts:   make(map[string][]string),
		mistify: pb.NewMistifyClient(conn),
	}
}

func (r *RProxy) Add(name string, ips []string) error {
	if len(ips) == 0 {
		return fmt.Errorf("no ips given")
	}

	r.hl.Lock()
	defer r.hl.Unlock()

	// if function exists, we should update!
	// if _, ok := r.hosts[name]; ok {
	// 	return fmt.Errorf("function already exists")
	// }

	r.hosts[name] = ips
	return nil
}

func (r *RProxy) Del(name string) error {
	r.hl.Lock()
	defer r.hl.Unlock()

	if _, ok := r.hosts[name]; !ok {
		return fmt.Errorf("function not found")
	}

	delete(r.hosts, name)
	return nil
}

func (r *RProxy) Call(name string, payload []byte, async bool, bypass bool) (Status, []byte) {
	if bypass {
		return r.CallLocal(name, payload, async)
	}
	res, err := r.mistify.CallFunction(context.Background(), &pb.FunctionCall{
		FunctionIdentifier: name,
		Data:               string(payload),
		Async:              async,
	})
	if err != nil {
		log.Printf("error calling function: %v", err)
		return StatusError, nil
	}

	return StatusOK, []byte(res.Response)
}

func (r *RProxy) CallLocal(name string, payload []byte, async bool) (Status, []byte) {

	handler, ok := r.hosts[name]

	if !ok {
		log.Printf("function not found: %s", name)
		return StatusNotFound, nil
	}

	log.Printf("have handlers: %s", handler)

	// choose random handler
	h := handler[rand.Intn(len(handler))]

	log.Printf("chosen handler: %s", h)

	// call function
	if async {
		log.Printf("async request accepted")
		go func() {
			resp, err := http.Post(fmt.Sprintf("http://%s:8000/fn", h), "application/binary", bytes.NewBuffer(payload))

			if err != nil {
				return
			}

			resp.Body.Close()

			log.Printf("async request finished")
		}()
		return StatusAccepted, nil
	}

	// call function and return results
	log.Printf("sync request starting")
	resp, err := http.Post(fmt.Sprintf("http://%s:8000/fn", h), "application/binary", bytes.NewBuffer(payload))

	if err != nil {
		log.Print(err)
		return StatusError, nil
	}

	log.Printf("sync request finished")

	defer resp.Body.Close()
	res_body, err := io.ReadAll(resp.Body)

	if err != nil {
		log.Print(err)
		return StatusError, nil
	}

	// log.Printf("have response for sync request: %s", res_body)

	return StatusOK, res_body
}
