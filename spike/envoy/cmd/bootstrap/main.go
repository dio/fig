package main

import (
	"flag"
	"github.com/dio/fig/spike/envoy/bootstrap"
	"log"
	"os"
)

func main() {
	source := flag.String("bundle", "../../examples/config/waf.json", "bundle JSON")
	template := flag.String("template", "envoy.json", "Envoy template")
	output := flag.String("out", ".bin/envoy.json", "rendered bootstrap")
	flag.Parse()
	data, err := os.ReadFile(*source)
	if err != nil {
		log.Fatal(err)
	}
	tmpl, err := os.ReadFile(*template)
	if err != nil {
		log.Fatal(err)
	}
	rendered, err := bootstrap.Render(tmpl, data)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*output, rendered, 0600); err != nil {
		log.Fatal(err)
	}
}
