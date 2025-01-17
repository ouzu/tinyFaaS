package http

import (
	"io"
	"log"
	"net/http"

	"github.com/OpenFogStack/tinyFaaS/pkg/rproxy"
)

func Start(r *rproxy.RProxy, listenAddr string) {

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		p := req.URL.Path

		for p != "" && p[0] == '/' {
			p = p[1:]
		}

		async := req.Header.Get("X-tinyFaaS-Async") != ""
		bypass := req.Header.Get("X-mistify-bypass") != ""

		log.Printf("have request for path: %s (async: %v, bypass: %v)", p, async, bypass)

		req_body, err := io.ReadAll(req.Body)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Print(err)
			return
		}

		s, res, header := r.Call(p, req_body, async, bypass)

		w.Header().Add("X-Mistify-Data", header)

		switch s {
		case rproxy.StatusOK:
			w.WriteHeader(http.StatusOK)
			w.Write(res)
		case rproxy.StatusAccepted:
			w.WriteHeader(http.StatusAccepted)
		case rproxy.StatusNotFound:
			w.WriteHeader(http.StatusNotFound)
		case rproxy.StatusError:
			w.WriteHeader(http.StatusInternalServerError)
		}
	})

	log.Printf("Starting HTTP server on %s", listenAddr)
	err := http.ListenAndServe(listenAddr, mux)

	if err != nil {
		log.Fatal(err)
	}

	log.Print("HTTP server stopped")

}
