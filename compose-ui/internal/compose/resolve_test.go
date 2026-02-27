package compose

import "testing"

func TestResolveByLabels(t *testing.T) {
	labels := map[string]string{
		LabelProjectName: "demo",
		LabelWorkingDir:  "/srv/demo",
		LabelConfigFiles: "docker-compose.yml",
	}
	res := Resolve(labels, nil)
	if !res.Editable {
		t.Fatalf("expected editable")
	}
	if res.ComposeFile != "/srv/demo/docker-compose.yml" {
		t.Fatalf("unexpected compose file: %s", res.ComposeFile)
	}
}

func TestResolveFallbackMount(t *testing.T) {
	res := Resolve(map[string]string{}, []Mount{{Source: "/opt/app"}})
	if !res.Editable {
		t.Fatalf("expected editable")
	}
	if res.WorkingDir != "/opt/app" {
		t.Fatalf("unexpected working dir: %s", res.WorkingDir)
	}
}
