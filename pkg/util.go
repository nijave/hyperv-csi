package pkg

import (
	"encoding/json"
	"k8s.io/klog/v2"
)

func logRequest(method string, value any) {
	jsonRequest, _ := json.Marshal(value)
	klog.InfoS("received request", "method", method, "request", jsonRequest)
}
