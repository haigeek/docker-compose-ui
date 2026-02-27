package model

type Project struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	ComposeFilePath string    `json:"composeFilePath,omitempty"`
	WorkingDir      string    `json:"workingDir,omitempty"`
	Services        []Service `json:"services"`
	Editable        bool      `json:"editable"`
}

type Service struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContainerID string `json:"containerId"`
	Status      string `json:"status"`
	Image       string `json:"image"`
}

type ComposeFile struct {
	Content    string `json:"content"`
	Mtime      int64  `json:"mtime"`
	Size       int64  `json:"size"`
	BackupPath string `json:"backupPath,omitempty"`
}

type ActionResult struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	DurationMS int64  `json:"durationMs"`
}

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Detail    string `json:"detail,omitempty"`
	Retryable bool   `json:"retryable"`
}

type Image struct {
	ID       string   `json:"id"`
	RepoTags []string `json:"repoTags"`
	Size     int64    `json:"size"`
	Created  int64    `json:"created"`
	Used     bool     `json:"used"`
}

type ImageDeleteResult struct {
	ImageID string `json:"imageId"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Status  string `json:"status"`
	Project string `json:"project"`
}
