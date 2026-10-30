# 📖 SysDashServer — API Specification & Integration Guide

> **SysDashServer** là backend giám sát hệ thống thời gian thực (CPU, RAM, Disk, Network, Top Processes, Docker) viết bằng **Golang**, hỗ trợ cơ chế xác thực **JWT**, phân quyền **RBAC (Admin / Viewer)**, và **WebSocket Streaming**.

---

## 📌 1. Thông Tin Chung (General Info)

- **Base URL**: `http://<server-ip>:8095/api/v1`
- **WebSocket URL**: `ws://<server-ip>:8095/api/v1/ws/metrics`
- **Content-Type**: `application/json`
- **Default Admin Account**:
  - **Username**: `admin`
  - **Password**: `admin123`
  - **Role**: `admin`

---

## 🔐 2. Chuẩn Xác Thực (Authentication Standard)

Hầu hết các API (ngoại trừ Health và Login) đều yêu cầu đính kèm token xác thực:

### Header Chuẩn (REST API):
```http
Authorization: Bearer <your_jwt_token>
```

### URL Query Param (Dành riêng cho WebSocket / Client giới hạn header):
```http
ws://<server-ip>:8095/api/v1/ws/metrics?token=<your_jwt_token>
```

### Phân quyền (RBAC Roles):
- **`viewer`**: Quyền chỉ đọc (xem metrics, biểu đồ lịch sử, danh sách tiến trình, docker, alerts, kết nối websocket).
- **`admin`**: Toàn quyền (xem toàn bộ hệ thống + CRUD users + điều chỉnh cấu hình cảnh báo alerts).

---

## ⚠️ 3. Chuẩn Phản Hồi Lỗi (Error Response Format)

Mọi phản hồi lỗi từ server đều tuân theo cấu trúc JSON thống nhất:

```json
{
  "error": "Mô tả lỗi ngắn gọn",
  "details": "Chi tiết lỗi kỹ thuật (nếu có)"
}
```

**Mã trạng thái HTTP phổ biến:**
- `400 Bad Request`: Sai định dạng tham số hoặc thiếu trường bắt buộc.
- `401 Unauthorized`: Token không tồn tại, hết hạn hoặc tài khoản/mật khẩu sai.
- `403 Forbidden`: Tài khoản không đủ quyền hạn (ví dụ `viewer` cố gọi API của `admin`).
- `404 Not Found`: Không tìm thấy tài nguyên (User ID không tồn tại, v.v.).
- `409 Conflict`: Trùng lặp dữ liệu (Username đã được đăng ký).
- `500 Internal Server Error`: Lỗi xử lý nội bộ phía server.

---

---

## 🐳 3.1. Hướng Dẫn Chạy Docker Trên Linux VPS (Đo Chuẩn 100%)

Khi ứng dụng giám sát chạy bên trong Docker container, mặc định Docker sẽ **cô lập hoàn toàn** tài nguyên (Network bridge ảo, ổ đĩa overlay ảo, PID ảo, không có quyền truy cập socket Docker).

Để ứng dụng đo **chính xác 100% tài nguyên thực tế của VPS**, bắt buộc phải chạy container với các cờ Host Mount sau:

### Cách 1: Sử dụng Docker Compose (Khuyên dùng)
Dự án đã có sẵn file docker-compose.yml, bạn chỉ cần chạy:
``bash
docker compose up -d --build
``

### Cách 2: Sử dụng lệnh docker run trực tiếp:
``bash
docker run -d \
  --name sysdash-server \
  --restart unless-stopped \
  --net=host \
  --pid=host \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v /proc:/host/proc:ro \
  -v /sys:/host/sys:ro \
  -v /:/host:ro \
  -v E:\JAVA\Android studio\SysDashServer/sysdash.db:/app/sysdash.db \
  -e HOST_PROC=/host/proc \
  -e HOST_SYS=/host/sys \
  -e HOST_ROOT=/host \
  -e PORT=8095 \
  sysdash-server
``

#### 💡 Giải thích các cờ bắt buộc:
1. **--net=host**: Dùng trực tiếp stack mạng của VPS để đo đúng băng thông Internet Upload/Download thật (thay vì card mạng ảo 172.17.0.x).
2. **--pid=host**: Dùng bảng tiến trình của VPS để hiển thị danh sách TopProcesses của toàn bộ máy chủ.
3. **-v /var/run/docker.sock:/var/run/docker.sock:ro**: Cho phép ứng dụng đọc Docker Engine API để lấy danh sách Docker Images & Containers.
4. **-v /:/host:ro + -e HOST_ROOT=/host**: Mount ổ cứng thực tế của VPS để đo đúng dung lượng Disk (thay vì đo phân vùng overlay ảo của container).
5. **-v /proc:/host/proc:ro + -e HOST_PROC=/host/proc**: Đọc trực tiếp /proc của VPS để đo chính xác RAM và CPU theo chuẩn htop.

---
### 🩺 3.2. Đối chiếu số liệu với `htop` / `free` / `df` / `docker` (Diagnostics)

> [!IMPORTANT]
> `Dockerfile` build bằng `git clone` từ GitHub và `docker-compose.yml` dùng image `phamvandinh/sysdash-server:v1.0`. Mọi sửa đổi local **chỉ có hiệu lực trên VPS sau khi**: `git push` → build lại image (`--no-cache` hoặc đổi `CACHE_BUST`) → push image → `docker compose pull && docker compose up -d`.

**Đơn vị:** API trả về **bytes**. `htop`, `free -h`, `df -h`, `docker stats` dùng hệ **1024** (GiB/MiB); `docker images` dùng hệ **1000** (GB/MB). Frontend nên dùng sẵn các trường `*_formatted` hoặc chia cho `1024`, **không chia cho `1000`** (nếu không số sẽ lớn hơn htop ~7%).

| Chỉ số API | Lệnh đối chiếu trên VPS | Công thức |
|---|---|---|
| `mem_used` | `free -b` (cột `used`) | `MemTotal - MemFree - Buffers - Cached - SReclaimable` |
| `mem_available` / `mem_buff_cache` | `free -b` (cột `available` / `buff/cache`) | lấy từ `/proc/meminfo` |
| `partitions[].total/used/free` | `df -B1 /` | `Size / Used / Avail`, `used_percent = Used/(Used+Avail)` |
| `containers[].mem_usage` | `docker stats --no-stream` | `usage - inactive_file` (cgroup v2) / `usage - total_inactive_file` (v1) |
| `images[].size` | `docker images` | trường `Size` của Docker Engine API (đơn vị SI) |

#### `GET /system/diagnostics` *(Admin Only)*
Trả về giá trị thô của `/proc/meminfo`, biến môi trường `HOST_*`, việc có đọc được mount table của host hay không, và **lý do chấp nhận/bỏ qua từng mount** khi tính Disk.

```json
{
  "running_in_docker": true,
  "host_root": "/host",
  "host_mountinfo_readable": "yes",
  "meminfo_raw_bytes": { "MemTotal": 4108169216, "MemFree": 312000512, "Buffers": 98000896, "Cached": 1650003968, "SReclaimable": 160002048 },
  "memory_computed": { "total": 4108169216, "used_like_free_htop": 1888161792, "used_formatted": "1.76 GiB", "total_formatted": "3.83 GiB" },
  "partitions_scan": [
    { "device": "/dev/vda1", "mountpoint": "/", "stat_path": "/host", "display": "/", "decision": "accept" },
    { "device": "overlay", "mountpoint": "/var/lib/docker/overlay2/.../merged", "decision": "skip: virtual/pseudo filesystem" }
  ]
}
```

---
## 🚀 4. Chi Tiết Danh Sách API (API Endpoints & Examples)

---

### 🟢 4.1. Health Check

#### `GET /health`
Kiểm tra máy chủ có đang hoạt động hay không (Public, không cần Auth).

- **Response `200 OK`**:
```json
{
  "service": "SysDashServer",
  "status": "ok"
}
```

---

### 🔑 4.2. Authentication & Profile

#### `POST /auth/login`
Đăng nhập lấy JWT Bearer token.

- **Request Body**:
```json
{
  "username": "admin",
  "password": "admin123"
}
```

- **Response `200 OK`**:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": 1730380800,
  "user": {
    "id": 1,
    "username": "admin",
    "full_name": "Administrator",
    "role": "admin",
    "created_at": "2026-10-30T20:10:00Z",
    "updated_at": "2026-10-30T20:10:00Z"
  }
}
```

---

#### `GET /auth/me`
Lấy thông tin tài khoản đang đăng nhập.

- **Headers**: `Authorization: Bearer <token>`
- **Response `200 OK`**:
```json
{
  "user": {
    "id": 1,
    "username": "admin",
    "full_name": "Administrator",
    "role": "admin",
    "created_at": "2026-10-30T20:10:00Z",
    "updated_at": "2026-10-30T20:10:00Z"
  }
}
```

---

### 🖥️ 4.3. System & Hardware Metrics

#### `GET /system/info`
Lấy thông tin cấu hình phần cứng tĩnh (Tên chip CPU, Số nhân vật lý, Số luồng logic, Tốc độ xung nhịp MHz / GHz) và hệ điều hành máy chủ.

- **Headers**: `Authorization: Bearer <token>`
- **Response `200 OK`**:
```json
{
  "hostname": "DINH-PHAM",
  "os": "windows",
  "platform": "Microsoft Windows 11 Pro",
  "platform_family": "Standalone Workstation",
  "platform_version": "25H2",
  "kernel_version": "10.0.26200.9711 Build 26200.9711",
  "kernel_arch": "x86_64",
  "uptime": 15831,
  "boot_time": 1793357861,
  "cpu_model": "Intel(R) Core(TM) i7-14700K",
  "cpu_physical_cores": 20,
  "cpu_logical_cores": 28,
  "cpu_cores": 28,
  "cpu_mhz": 3400.0,
  "cpu_speed_ghz": 3.4
}
```

##### 📋 Bảng giải thích chi tiết các thông số CPU:
| Trường JSON | Kiểu dữ liệu | Ý nghĩa | Ví dụ thực tế |
|---|---|---|---|
| `cpu_model` | string | Tên chip vi xử lý | `"Intel(R) Core(TM) i7-14700K"` |
| `cpu_physical_cores` | int | **Số nhân vật lý** của CPU (Physical Cores) | `20` |
| `cpu_logical_cores` | int | **Số luồng** xử lý logic (Threads) | `28` |
| `cpu_cores` | int | Tổng số luồng/nhân logic (tương đương logical cores) | `28` |
| `cpu_mhz` | float | **Tốc độ xử lý** tính theo **MHz** | `3400.0` |
| `cpu_speed_ghz` | float | **Tốc độ xử lý** quy đổi ra **GHz** | `3.4` |

---

#### `GET /metrics/current`
Lấy ảnh chụp số liệu phần cứng tức thời (chứa `host_info`, CPU %, % từng nhân, RAM, Swap, Đĩa cứng, Mạng, Top 5 Processes) với đầy đủ cả giá trị thô (**bytes**) và chuỗi định dạng sẵn (**1024-based formatted strings** khớp với `htop`, `free -h`, `df -h`).

- **Headers**: `Authorization: Bearer <token>`
- **Response `200 OK`**:
```json
{
  "timestamp": "2026-10-31T03:00:00.123456Z",
  "host_info": {
    "hostname": "ubuntu-vps",
    "os": "linux",
    "platform": "ubuntu",
    "platform_family": "debian",
    "platform_version": "22.04",
    "kernel_version": "5.15.0-105-generic",
    "kernel_arch": "x86_64",
    "uptime": 158310,
    "boot_time": 1793357861,
    "cpu_model": "Intel(R) Xeon(R) Gold 6148 CPU @ 2.40GHz",
    "cpu_physical_cores": 2,
    "cpu_logical_cores": 4,
    "cpu_cores": 4,
    "cpu_mhz": 2400.0,
    "cpu_speed_ghz": 2.4
  },
  "cpu_percent": 14.85,
  "cpu_cores_percent": [12.5, 8.3, 25.0, 10.1],
  "mem_total": 4108169216,
  "mem_used": 1888161792,
  "mem_free": 2220007424,
  "mem_available": 2220007424,
  "mem_buff_cache": 1748004864,
  "mem_used_percent": 45.96,
  "mem_total_formatted": "3.83 GiB",
  "mem_used_formatted": "1.76 GiB",
  "swap_total": 2147483648,
  "swap_used": 536870912,
  "swap_percent": 25.0,
  "partitions": [
    {
      "device": "/dev/vda1",
      "mountpoint": "/",
      "fstype": "ext4",
      "total": 83057483776,
      "used": 30582882304,
      "free": 52474601472,
      "used_percent": 36.82,
      "total_formatted": "77.35 GiB",
      "used_formatted": "28.48 GiB",
      "free_formatted": "48.87 GiB"
    }
  ],
  "net_bytes_sent": 1450239044,
  "net_bytes_recv": 9823485023,
  "net_upload_rate": 125430.5,
  "net_download_rate": 845210.2,
  "top_processes": [
    {
      "pid": 28224,
      "name": "sysdash-server",
      "cpu_percent": 0.85,
      "memory_percent": 1.12,
      "memory_rss": 45900000,
      "status": "running"
    },
    {
      "pid": 1698,
      "name": "dockerd",
      "cpu_percent": 0.40,
      "memory_percent": 2.14,
      "memory_rss": 87800000,
      "status": "running"
    }
  ]
}
```

##### 📋 Bảng giải thích chi tiết các trường (`RealtimeMetric` & `PartitionStat`):
| Trường JSON | Kiểu dữ liệu | Đơn vị | Ý nghĩa & Lệnh đối chiếu trên VPS |
|---|---|---|---|
| `mem_total` | uint64 | bytes | Tổng RAM vật lý (`MemTotal` trong `free -b` / `/proc/meminfo`) |
| `mem_used` | uint64 | bytes | RAM thực sự hệ thống & ứng dụng đang dùng (cột `used` của `free -b`, khớp thanh xanh của `htop`) |
| `mem_free` | uint64 | bytes | RAM chưa phân bổ (`mem_total - mem_used`) |
| `mem_available` | uint64 | bytes | RAM khả dụng có thể cấp phát mà không cần swap (cột `available` của `free -b`) |
| `mem_buff_cache` | uint64 | bytes | Bộ đệm kernel và đĩa đệm (cột `buff/cache` của `free -b`) |
| `mem_used_percent` | float64 | % | Tỷ lệ phần trăm RAM sử dụng (`mem_used / mem_total * 100`) |
| `mem_total_formatted` | string | Chuỗi GiB | Tổng RAM định dạng hệ 1024 (VD: `3.83 GiB` khớp `htop`) |
| `mem_used_formatted` | string | Chuỗi GiB | RAM sử dụng định dạng hệ 1024 (VD: `1.76 GiB` khớp `htop`) |
| `partitions[].device` | string | - | Tên phân vùng thiết bị (VD: `/dev/vda1`, `C:`) |
| `partitions[].mountpoint` | string | - | Điểm gắn ổ đĩa (`/`, `/boot`, `E:`) |
| `partitions[].fstype` | string | - | Định dạng hệ thống tệp (`ext4`, `xfs`, `NTFS`) |
| `partitions[].total` | uint64 | bytes | Dung lượng phân vùng (cột `1B-blocks` của `df -B1`) |
| `partitions[].used` | uint64 | bytes | Dung lượng đã dùng (cột `Used` của `df -B1`) |
| `partitions[].free` | uint64 | bytes | Dung lượng trống cho người dùng (cột `Available` của `df -B1`) |
| `partitions[].used_percent` | float64 | % | Tỷ lệ sử dụng (cột `Use%` của `df -h`: `used / (used + free) * 100`) |
| `partitions[].total_formatted` | string | Chuỗi GiB | Tổng dung lượng định dạng 1024-based (VD: `77.35 GiB` khớp `df -h`) |
| `partitions[].used_formatted` | string | Chuỗi GiB | Dung lượng dùng định dạng 1024-based (VD: `28.48 GiB` khớp `df -h`) |
| `partitions[].free_formatted` | string | Chuỗi GiB | Dung lượng trống định dạng 1024-based (VD: `48.87 GiB` khớp `df -h`) |

---

#### `GET /metrics/history`
Lấy dữ liệu chuỗi thời gian để vẽ đồ thị (hỗ trợ Downsampling / Bucket Aggregation tối ưu hiệu năng).

- **Query Parameters**:
  | Param | Kiểu | Mặc định | Mô tả |
  |---|---|---|---|
  | `range` | string | `1h` | Khoảng thời gian: `1h`, `6h`, `12h`, `24h`, `7d`, `30d` |
  | `from` | string | - | Mốc bắt đầu ISO RFC3339 (VD: `2026-10-30T00:00:00Z`) |
  | `to` | string | - | Mốc kết thúc ISO RFC3339 |
  | `bucket` | string | `auto` | Gom nhóm trung bình: `none`, `1m`, `5m`, `15m`, `1h`, `auto` |
  | `limit` | int | `500` | Số lượng điểm tối đa (1 - 2000) |

- **Response `200 OK`**:
```json
{
  "range": "24h",
  "from": "2026-10-29T20:30:00Z",
  "to": "2026-10-30T20:30:00Z",
  "bucket": "15m0s",
  "count": 96,
  "raw_count": 5760,
  "metrics": [
    {
      "id": 0,
      "timestamp": "2026-10-29T20:30:00Z",
      "cpu_percent": 12.45,
      "mem_total": 34185805824,
      "mem_used": 24271922135,
      "mem_used_percent": 71.0,
      "disk_total": 511000000000,
      "disk_used": 285000000000,
      "disk_percent": 55.77,
      "net_bytes_sent": 1450239044,
      "net_bytes_recv": 9823485023,
      "net_upload_rate": 84200.5,
      "net_download_rate": 524100.0
    }
  ]
}
```

---

#### `GET /metrics/summary`
Lấy thống kê Min, Max, Average của các chỉ số trong một khoảng thời gian (dành cho Card tóm tắt / Dashboard Overview).

- **Query Parameters**: `range` (`1h`, `6h`, `12h`, `24h`, `7d`, `30d`) hoặc `from`, `to`.
- **Response `200 OK`**:
```json
{
  "time_range": "24h",
  "from": "2026-10-29T20:30:00Z",
  "to": "2026-10-30T20:30:00Z",
  "sample_count": 5760,
  "avg_cpu": 16.82,
  "min_cpu": 3.10,
  "max_cpu": 89.45,
  "avg_mem_percent": 72.40,
  "min_mem_percent": 65.20,
  "max_mem_percent": 84.10,
  "avg_disk_percent": 55.77,
  "max_disk_percent": 56.12,
  "avg_upload_rate": 95420.15,
  "max_upload_rate": 15420000.0,
  "avg_download_rate": 624100.80,
  "max_download_rate": 45800000.0
}
```

---

### ⚙️ 4.4. Quản Lý Tiến Trình (Processes)

#### `GET /system/processes`
Tìm kiếm, sắp xếp và phân trang các tiến trình hệ thống đang chạy.

- **Query Parameters**:
  | Param | Kiểu | Mặc định | Mô tả |
  |---|---|---|---|
  | `search` | string | `""` | Tìm theo tên tiến trình hoặc PID (VD: `chrome`, `java`, `28224`) |
  | `sort` | string | `cpu` | Trường sắp xếp: `cpu`, `memory`, `rss`, `name`, `pid` |
  | `order` | string | `desc` | Thứ tự: `desc` (giảm dần), `asc` (tăng dần) |
  | `limit` | int | `10` | Số lượng tiến trình trả về (1 - 100) |

- **Response `200 OK`**:
```json
{
  "count": 2,
  "search": "node",
  "sort": "cpu",
  "order": "desc",
  "processes": [
    {
      "pid": 14210,
      "name": "node.exe",
      "cpu_percent": 18.25,
      "memory_percent": 4.12,
      "memory_rss": 1408000000,
      "status": "running"
    },
    {
      "pid": 19400,
      "name": "node.exe",
      "cpu_percent": 2.10,
      "memory_percent": 1.50,
      "memory_rss": 512000000,
      "status": "running"
    }
  ]
}
```

---

### 🐳 4.5. Docker Monitoring

#### `GET /docker/images`
Lấy danh sách các Docker Image trên máy chủ, bao gồm dung lượng ổ đĩa (Disk Size) và **tổng dung lượng RAM đang tiêu thụ** bởi các container đang chạy từ image này.

- **Headers**: `Authorization: Bearer <token>`
- **Response `200 OK` (Khi Docker đang chạy)**:
```json
{
  "available": true,
  "count": 2,
  "images": [
    {
      "id": "c13b28b6d803",
      "repo_tags": [
        "nginx:alpine"
      ],
      "size": 23592960,
      "size_formatted": "22.50 MB",
      "created": 1729000000,
      "containers": 1,
      "running_containers": 1,
      "mem_usage": 47349760,
      "mem_formatted": "45.16 MB"
    },
    {
      "id": "8485203fa120",
      "repo_tags": [
        "postgres:16-alpine"
      ],
      "size": 185420000,
      "size_formatted": "176.83 MB",
      "created": 1728500000,
      "containers": 1,
      "running_containers": 1,
      "mem_usage": 89128960,
      "mem_formatted": "85.00 MB"
    }
  ]
}
```

- **Response `200 OK` (Khi Docker chưa chạy hoặc không có socket)**:
```json
{
  "available": false,
  "count": 0,
  "images": [],
  "message": "Docker daemon is not running or socket is not accessible"
}
```

---

#### `GET /docker/containers`
Lấy danh sách tất cả Containers (đang chạy hoặc đã dừng) kèm theo **chi tiết RAM thực tế (Used), giới hạn RAM (Limit), % RAM và % CPU** chuẩn theo công thức `docker stats`.

- **Headers**: `Authorization: Bearer <token>`
- **Response `200 OK`**:
```json
{
  "available": true,
  "count": 1,
  "containers": [
    {
      "id": "b78d23419a2b",
      "names": [
        "my-nginx-web"
      ],
      "image": "nginx:alpine",
      "image_id": "sha256:c13b28b6d803...",
      "command": "/docker-entrypoint.sh nginx -g 'daemon off;'",
      "created": 1729500000,
      "state": "running",
      "status": "Up 3 days",
      "labels": {
        "maintainer": "NGINX Docker Maintainers"
      },
      "mem_usage": 47349760,
      "mem_limit": 2097152000,
      "mem_percent": 2.26,
      "mem_usage_formatted": "45.16 MB / 1.95 GB",
      "cpu_percent": 0.45
    }
  ]
}
```

---

### ⚡ 4.6. Real-time Streaming (WebSocket)

#### `WS /ws/metrics?token=<jwt>`
Kết nối WebSocket để nhận dữ liệu đẩy về liên tục mỗi **2 giây**.

- **URL**: `ws://<server-ip>:8095/api/v1/ws/metrics?token=<jwt>`
- **Cơ chế**:
  - Gửi dữ liệu snapshot ngay khi vừa bắt tay (handshake) thành công.
  - Tự động đẩy payload `RealtimeMetric` mỗi 2 giây.
- **Message Payload nhận được (JSON)**:
```json
{
  "timestamp": "2026-10-31T03:00:02Z",
  "host_info": {
    "hostname": "ubuntu-vps",
    "os": "linux",
    "platform": "ubuntu",
    "platform_version": "22.04",
    "cpu_model": "Intel(R) Xeon(R) Gold 6148 CPU @ 2.40GHz",
    "cpu_physical_cores": 2,
    "cpu_logical_cores": 4,
    "cpu_cores": 4,
    "cpu_mhz": 2400.0,
    "cpu_speed_ghz": 2.4,
    "uptime": 158310
  },
  "cpu_percent": 15.20,
  "cpu_cores_percent": [12.0, 14.5, 8.0, 22.1],
  "mem_total": 4108169216,
  "mem_used": 1888161792,
  "mem_free": 2220007424,
  "mem_available": 2220007424,
  "mem_buff_cache": 1748004864,
  "mem_used_percent": 45.96,
  "mem_total_formatted": "3.83 GiB",
  "mem_used_formatted": "1.76 GiB",
  "swap_total": 2147483648,
  "swap_used": 536870912,
  "swap_percent": 25.0,
  "partitions": [
    {
      "device": "/dev/vda1",
      "mountpoint": "/",
      "fstype": "ext4",
      "total": 83057483776,
      "used": 30582882304,
      "free": 52474601472,
      "used_percent": 36.82,
      "total_formatted": "77.35 GiB",
      "used_formatted": "28.48 GiB",
      "free_formatted": "48.87 GiB"
    }
  ],
  "net_bytes_sent": 1450250000,
  "net_bytes_recv": 9823500000,
  "net_upload_rate": 5500.0,
  "net_download_rate": 7500.0,
  "top_processes": [
    {
      "pid": 28224,
      "name": "sysdash-server",
      "cpu_percent": 0.85,
      "memory_percent": 1.12,
      "memory_rss": 45900000,
      "status": "running"
    }
  ]
}
```

---

### 🚨 4.7. Cảnh Báo & Ngưỡng Quá Tải (Alerts)

#### `GET /alerts`
Lọc lịch sử cảnh báo quá tải phần cứng (CPU, RAM, Disk).

- **Query Parameters**:
  | Param | Kiểu | Mặc định | Mô tả |
  |---|---|---|---|
  | `type` | string | `""` | Lọc loại: `CPU`, `MEMORY`, `DISK` |
  | `level` | string | `""` | Lọc cấp độ: `WARNING`, `CRITICAL` |
  | `range` | string | `""` | Khoảng thời gian: `1h`, `24h`, `7d`, `30d` |
  | `page` | int | `1` | Trang hiện tại |
  | `limit` | int | `20` | Số lượng bản ghi mỗi trang |

- **Response `200 OK`**:
```json
{
  "page": 1,
  "limit": 20,
  "total": 3,
  "total_pages": 1,
  "alerts": [
    {
      "id": 1,
      "timestamp": "2026-10-30T19:45:00Z",
      "type": "CPU",
      "level": "WARNING",
      "value": 94.5,
      "threshold": 90.0,
      "message": "CPU usage reached 94.5% exceeding threshold 90.0%"
    }
  ]
}
```

---

#### `GET /alerts/config`
Lấy cấu hình ngưỡng cảnh báo hiện tại.

- **Response `200 OK`**:
```json
{
  "id": 1,
  "cpu_percent": 90.0,
  "mem_percent": 90.0,
  "disk_percent": 90.0,
  "enabled": true,
  "updated_at": "2026-10-30T20:00:00Z"
}
```

---

#### `PUT /alerts/config` *(Admin Only)*
Cập nhật ngưỡng kích hoạt cảnh báo.

- **Headers**: `Authorization: Bearer <admin_token>`
- **Request Body**:
```json
{
  "cpu_percent": 85.0,
  "mem_percent": 90.0,
  "disk_percent": 95.0,
  "enabled": true
}
```
- **Response `200 OK`**:
```json
{
  "message": "threshold updated successfully",
  "threshold": {
    "id": 1,
    "cpu_percent": 85.0,
    "mem_percent": 90.0,
    "disk_percent": 95.0,
    "enabled": true,
    "updated_at": "2026-10-30T20:31:00Z"
  }
}
```

---

### 👥 4.8. Quản Lý Người Dùng *(Admin Only)*

#### `GET /users`
Tìm kiếm, lọc vai trò và phân trang danh sách người dùng.

- **Headers**: `Authorization: Bearer <admin_token>`
- **Query Parameters**:
  | Param | Kiểu | Mặc định | Mô tả |
  |---|---|---|---|
  | `search` | string | `""` | Tìm theo username hoặc họ tên |
  | `role` | string | `""` | Lọc nhóm quyền: `admin` hoặc `viewer` |
  | `page` | int | `1` | Số trang |
  | `limit` | int | `20` | Số tài khoản mỗi trang |

- **Response `200 OK`**:
```json
{
  "page": 1,
  "limit": 20,
  "total": 2,
  "total_pages": 1,
  "users": [
    {
      "id": 1,
      "username": "admin",
      "full_name": "Administrator",
      "role": "admin",
      "created_at": "2026-10-30T20:00:00Z",
      "updated_at": "2026-10-30T20:00:00Z"
    },
    {
      "id": 2,
      "username": "viewer1",
      "full_name": "Nhân viên trực ca",
      "role": "viewer",
      "created_at": "2026-10-30T20:05:00Z",
      "updated_at": "2026-10-30T20:05:00Z"
    }
  ]
}
```

---

#### `POST /users` (hoặc `/auth/register`)
Tạo tài khoản người dùng mới.

- **Headers**: `Authorization: Bearer <admin_token>`
- **Request Body**:
```json
{
  "username": "operator_hn",
  "password": "SecretPassword123",
  "full_name": "Vận hành Hà Nội",
  "role": "viewer"
}
```
- **Response `201 Created`**:
```json
{
  "message": "user created successfully",
  "user": {
    "id": 3,
    "username": "operator_hn",
    "full_name": "Vận hành Hà Nội",
    "role": "viewer",
    "created_at": "2026-10-30T20:32:00Z",
    "updated_at": "2026-10-30T20:32:00Z"
  }
}
```

---

#### `PUT /users/:id`
Cập nhật thông tin, thay đổi mật khẩu hoặc đổi quyền người dùng.

- **Headers**: `Authorization: Bearer <admin_token>`
- **Request Body** (tất cả các trường đều optional):
```json
{
  "full_name": "Tên Mới",
  "role": "admin",
  "password": "NewPassword456"
}
```
- **Response `200 OK`**:
```json
{
  "message": "user updated successfully",
  "user": {
    "id": 3,
    "username": "operator_hn",
    "full_name": "Tên Mới",
    "role": "admin",
    "created_at": "2026-10-30T20:32:00Z",
    "updated_at": "2026-10-30T20:33:00Z"
  }
}
```

---

#### `DELETE /users/:id`
Xóa tài khoản người dùng (Admin không thể tự xóa tài khoản của chính mình).

- **Headers**: `Authorization: Bearer <admin_token>`
- **Response `200 OK`**:
```json
{
  "message": "user deleted successfully"
}
```

---

## 💻 5. Hướng Dẫn Tích Hợp Frontend (Code Examples)

### 🌐 5.1. JavaScript / TypeScript (Axios + WebSocket)

```typescript
import axios from 'axios';

const API_BASE = 'http://localhost:8095/api/v1';
let authToken = '';

// 1. Static Host & Hardware Info
export interface HostInfo {
  hostname: string;
  os: string;
  platform: string;
  platform_family: string;
  platform_version: string;
  kernel_version: string;
  kernel_arch: string;
  uptime: number;
  boot_time: number;
  cpu_model: string;
  cpu_physical_cores: number; // Số nhân vật lý (VD: 2)
  cpu_logical_cores: number;  // Số luồng logic (VD: 4)
  cpu_cores: number;
  cpu_mhz: number;            // Xung nhịp MHz (VD: 2400.0)
  cpu_speed_ghz: number;      // Xung nhịp GHz (VD: 2.40)
}

// 2. Disk Partition Info (chuẩn df -h & df -B1)
export interface PartitionStat {
  device: string;
  mountpoint: string;
  fstype: string;
  total: number;           // bytes
  used: number;            // bytes
  free: number;            // bytes
  used_percent: number;    // %
  total_formatted: string; // "77.35 GiB"
  used_formatted: string;  // "28.48 GiB"
  free_formatted: string;  // "48.87 GiB"
}

// 3. Process Info
export interface ProcessInfo {
  pid: number;
  name: string;
  cpu_percent: number;
  memory_percent: number;
  memory_rss: number;
  status: string;
}

// 4. Real-time Metric Full Payload (khớp 100% internal/models/metric.go)
export interface RealtimeMetric {
  timestamp: string;
  host_info: HostInfo;
  cpu_percent: number;
  cpu_cores_percent: number[];
  mem_total: number;           // bytes (MemTotal)
  mem_used: number;            // bytes (used giống htop & free -b)
  mem_free: number;            // bytes
  mem_available: number;       // bytes (available)
  mem_buff_cache: number;      // bytes (buff/cache)
  mem_used_percent: number;
  mem_total_formatted: string; // "3.83 GiB" (hiển thị trực tiếp UI)
  mem_used_formatted: string;  // "1.76 GiB"
  swap_total: number;
  swap_used: number;
  swap_percent: number;
  partitions: PartitionStat[];
  net_bytes_sent: number;
  net_bytes_recv: number;
  net_upload_rate: number;
  net_download_rate: number;
  top_processes: ProcessInfo[];
}

// 5. Đăng nhập
export async function login(username: string, password: string) {
  const res = await axios.post(`${API_BASE}/auth/login`, { username, password });
  authToken = res.data.token;
  localStorage.setItem('sysdash_token', authToken);
  return res.data;
}

// 6. Lấy dữ liệu Realtime Snapshot
export async function getCurrentMetrics(): Promise<RealtimeMetric> {
  const res = await axios.get(`${API_BASE}/metrics/current`, {
    headers: { Authorization: `Bearer ${authToken}` }
  });
  return res.data;
}

// 7. Kết nối WebSocket thời gian thực (nhận RealtimeMetric mỗi 2s)
export function connectRealtimeStream(onData: (metric: RealtimeMetric) => void) {
  const ws = new WebSocket(`ws://localhost:8095/api/v1/ws/metrics?token=${authToken}`);

  ws.onmessage = (event) => {
    const metric: RealtimeMetric = JSON.parse(event.data);
    onData(metric);
  };

  ws.onerror = (err) => console.error('WebSocket Error:', err);
  ws.onclose = () => console.log('WebSocket Closed');
  return ws;
}
```

---

### 📱 5.2. Android Kotlin (Retrofit + OkHttp)

#### Data Models Kotlin (khớp 100% `internal/models/metric.go`):
```kotlin
data class HostInfo(
    val hostname: String,
    val os: String,
    val platform: String,
    val platform_family: String,
    val platform_version: String,
    val kernel_version: String,
    val kernel_arch: String,
    val uptime: Long,
    val boot_time: Long,
    val cpu_model: String,
    val cpu_physical_cores: Int,
    val cpu_logical_cores: Int,
    val cpu_cores: Int,
    val cpu_mhz: Double,
    val cpu_speed_ghz: Double
)

data class PartitionStat(
    val device: String,
    val mountpoint: String,
    val fstype: String,
    val total: Long,
    val used: Long,
    val free: Long,
    val used_percent: Double,
    val total_formatted: String,
    val used_formatted: String,
    val free_formatted: String
)

data class ProcessInfo(
    val pid: Int,
    val name: String,
    val cpu_percent: Double,
    val memory_percent: Float,
    val memory_rss: Long,
    val status: String
)

data class RealtimeMetric(
    val timestamp: String,
    val host_info: HostInfo,
    val cpu_percent: Double,
    val cpu_cores_percent: List<Double>,
    val mem_total: Long,
    val mem_used: Long,
    val mem_free: Long,
    val mem_available: Long,
    val mem_buff_cache: Long,
    val mem_used_percent: Double,
    val mem_total_formatted: String,
    val mem_used_formatted: String,
    val swap_total: Long,
    val swap_used: Long,
    val swap_percent: Double,
    val partitions: List<PartitionStat>,
    val net_bytes_sent: Long,
    val net_bytes_recv: Long,
    val net_upload_rate: Double,
    val net_download_rate: Double,
    val top_processes: List<ProcessInfo>
)
```

#### Interface API Retrofit:
```kotlin
interface SysDashApi {
    @POST("auth/login")
    suspend fun login(@Body body: LoginRequest): Response<LoginResponse>

    @GET("system/info")
    suspend fun getSystemInfo(@Header("Authorization") auth: String): Response<HostInfo>

    @GET("metrics/current")
    suspend fun getCurrentMetrics(@Header("Authorization") auth: String): Response<RealtimeMetric>

    @GET("metrics/history")
    suspend fun getHistory(
        @Header("Authorization") auth: String,
        @Query("range") range: String = "24h",
        @Query("bucket") bucket: String = "15m"
    ): Response<HistoryResponse>

    @GET("system/processes")
    suspend fun getProcesses(
        @Header("Authorization") auth: String,
        @Query("sort") sort: String = "memory",
        @Query("order") order: String = "desc",
        @Query("limit") limit: Int = 20
    ): Response<ProcessListResponse>
}
```

#### Kết nối WebSocket trên Android (OkHttp):
```kotlin
val client = OkHttpClient()
val request = Request.Builder()
    .url("ws://10.0.2.2:8095/api/v1/ws/metrics?token=")
    .build()

val listener = object : WebSocketListener() {
    override fun onMessage(webSocket: WebSocket, text: String) {
        val metric = Gson().fromJson(text, RealtimeMetric::class.java)
        runOnUiThread {
            // Cập nhật UI với số liệu đã format chuẩn htop / df -h:
            // metric.mem_used_formatted (VD: "1.76 GiB")
            // metric.partitions[0].used_formatted (VD: "28.48 GiB")
            updateDashboard(metric)
        }
    }
}

val ws = client.newWebSocket(request, listener)
```
