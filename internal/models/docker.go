package models

// DockerImageInfo represents docker image details with disk size and active RAM usage
type DockerImageInfo struct {
	ID                string   `json:"id"`
	RepoTags          []string `json:"repo_tags"`
	Size              int64    `json:"size"`           // Dung lượng ổ đĩa (Disk bytes)
	SizeFormatted     string   `json:"size_formatted"` // VD: "185.40 MB"
	Created           int64    `json:"created"`
	Containers        int64    `json:"containers"`         // Tổng số containers của image này
	RunningContainers int      `json:"running_containers"` // Số container đang chạy
	MemUsage          uint64   `json:"mem_usage"`          // Tổng RAM thực tế đang dùng bởi các container (bytes)
	MemFormatted      string   `json:"mem_formatted"`      // Định dạng RAM (VD: "45.20 MiB")
}

// DockerContainerInfo represents running or stopped docker container details with CPU & RAM stats
type DockerContainerInfo struct {
	ID                string            `json:"id"`
	Names             []string          `json:"names"`
	Image             string            `json:"image"`
	ImageID           string            `json:"image_id"`
	Command           string            `json:"command"`
	Created           int64             `json:"created"`
	State             string            `json:"state"`  // VD: "running", "exited"
	Status            string            `json:"status"` // VD: "Up 2 hours"
	Labels            map[string]string `json:"labels"`
	MemUsage          uint64            `json:"mem_usage"`           // RAM thực tế đang dùng (bytes)
	MemLimit          uint64            `json:"mem_limit"`           // Giới hạn RAM được cấp hoặc tổng RAM host nếu không giới hạn (bytes)
	MemPercent        float64           `json:"mem_percent"`         // Phần trăm RAM (%)
	MemUsageFormatted string            `json:"mem_usage_formatted"` // VD: "45.20 MiB / 1.95 GiB"
	CPUPercent        float64           `json:"cpu_percent"`         // Phần trăm CPU chuẩn theo docker stats (Irix mode: 1 core = 100%)
}
