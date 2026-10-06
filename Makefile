.PHONY: native-test
native-test:
	go test ./bundle ./body ./match
	$(MAKE) -C spike/envoy native-test
