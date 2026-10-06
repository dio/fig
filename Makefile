.PHONY: native-test
native-test:
	go test ./bundle
	$(MAKE) -C spike/envoy native-test
