package models

// DockerImageInfo represents docker image details
type DockerImageInfo struct {
	ID          string   `json:"id"`
	RepoTags    []string `json:"repo_tags"`
	Size        int64    `json:"size"`
	Created     int64    `json:"created"`
	Containers  int64    `json:"containers"`
}

// DockerContainerInfo represents running or stopped docker container details
type DockerContainerInfo struct {
	ID         string            `json:"id"`
	Names      []string          `json:"names"`
	Image      string            `json:"image"`
	ImageID    string            `json:"image_id"`
	Command    string            `json:"command"`
	Created    int64             `json:"created"`
	State      string            `json:"state"`   // e.g. "running", "exited"
	Status     string            `json:"status"`  // e.g. "Up 2 hours"
	Labels     map[string]string `json:"labels"`
}
