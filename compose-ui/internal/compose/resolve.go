package compose

import (
	"path/filepath"
	"strings"
)

const (
	LabelProjectName = "com.docker.compose.project"
	LabelWorkingDir  = "com.docker.compose.project.working_dir"
	LabelConfigFiles = "com.docker.compose.project.config_files"
	LabelServiceName = "com.docker.compose.service"
)

type Mount struct {
	Source string
}

type Resolved struct {
	ProjectName string
	ComposeFile string
	WorkingDir  string
	Editable    bool
}

func Resolve(labels map[string]string, mounts []Mount) Resolved {
	res := Resolved{ProjectName: strings.TrimSpace(labels[LabelProjectName])}

	workingDir := strings.TrimSpace(labels[LabelWorkingDir])
	configFiles := strings.TrimSpace(labels[LabelConfigFiles])
	if workingDir != "" && configFiles != "" {
		first := strings.TrimSpace(strings.Split(configFiles, ",")[0])
		if first != "" {
			if filepath.IsAbs(first) {
				res.ComposeFile = first
				res.WorkingDir = filepath.Dir(first)
			} else {
				res.ComposeFile = filepath.Clean(filepath.Join(workingDir, first))
				res.WorkingDir = workingDir
			}
			res.Editable = true
		}
	}

	if !res.Editable && len(mounts) > 0 {
		for _, m := range mounts {
			src := strings.TrimSpace(m.Source)
			if src == "" {
				continue
			}
			for _, file := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
				res.ComposeFile = filepath.Join(src, file)
				res.WorkingDir = src
				res.Editable = true
				break
			}
			if res.Editable {
				break
			}
		}
	}

	if res.ProjectName == "" {
		if res.WorkingDir != "" {
			res.ProjectName = filepath.Base(res.WorkingDir)
		} else {
			res.ProjectName = "unknown"
		}
	}

	return res
}
