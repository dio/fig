package main

import (
	"flag"
	"log"
	"os"

	"github.com/dio/fig/bundle"
	"github.com/dio/fig/spike/envoy/bootstrap"
)

func main() {
	waf := flag.String("bundle", "../../examples/config/waf.json", "WAF bundle JSON")
	marker := flag.String("marker", "../../examples/config/marker.json", "Marker bundle JSON")
	template := flag.String("template", "envoy.json", "Envoy template")
	output := flag.String("out", ".bin/envoy.json", "rendered bootstrap")
	flag.Parse()
	read := func(path string) []byte {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		return data
	}
	bindings := []bootstrap.Binding{
		{Name: "WAF", Entry: bundle.Ref{Type: "fig.pipeline/v1alpha1", Name: "edge", Version: "1"}, Data: read(*waf)},
		{Name: "MARKER", Entry: bundle.Ref{Type: "fig.pipeline/v1alpha1", Name: "mark", Version: "1"}, Data: read(*marker)},
	}
	rendered, err := bootstrap.Render(read(*template), bindings...)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*output, rendered, 0600); err != nil {
		log.Fatal(err)
	}
}
