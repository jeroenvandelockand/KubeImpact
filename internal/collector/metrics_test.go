package collector

import "testing"

func TestParseDeprecatedAPIMetrics(t *testing.T) {
	data := []byte(`# HELP apiserver_requested_deprecated_apis Gauge of deprecated APIs.
apiserver_requested_deprecated_apis{group="extensions",removed_release="1.22",resource="ingresses",subresource="",version="v1beta1"} 1
apiserver_requested_deprecated_apis{group="extensions",removed_release="1.22",resource="ingresses",subresource="status",version="v1beta1"} 0
apiserver_requested_deprecated_apis{group="example.io",removed_release="1.30",resource="widgets",subresource="",version="v1alpha1"} NaN
`)
	requests, err := parseDeprecatedAPIMetrics(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].GroupVersion != "extensions/v1beta1" || requests[0].RemovedRelease != "1.22" {
		t.Fatalf("requests = %#v", requests)
	}
}
