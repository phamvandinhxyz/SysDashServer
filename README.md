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
Lấy ảnh chụp số liệu phần cứng tức thời (chứa `host_info`, CPU %, % từng nhân, RAM, Swap, Đĩa cứng, Mạng, Top 5 Processes).

- **Headers**: `Authorization: Bearer <token>`
- **Response `200 OK`**:
```json
{
  "timestamp": "2026-10-30T20:30:00.123456Z",
  "host_info": {
    "hostname": "DINH-PHAM",
    "os": "windows",
    "cpu_model": "Intel(R) Core(TM) i7-14700K",
    "cpu_physical_cores": 20,
    "cpu_logical_cores": 28,
    "cpu_cores": 28,
    "cpu_mhz": 3400.0,
    "cpu_speed_ghz": 3.4,
    "uptime": 15831
  },
  "cpu_percent": 14.85,
  "cpu_cores_percent": [12.5, 8.3, 25.0, 10.1, 4.2, 16.7],
  "mem_total": 34185805824,
  "mem_used": 25639354368,
  "mem_free": 8546451456,
  "mem_used_percent": 75.0,
  "swap_total": 4294967296,
  "swap_used": 1073741824,
  "swap_percent": 25.0,
  "partitions": [
    {
      "device": "C:",
      "mountpoint": "C:",
      "fstype": "NTFS",
      "total": 511000000000,
      "used": 285000000000,
      "free": 226000000000,
      "used_percent": 55.77
    },
    {
      "device": "E:",
      "mountpoint": "E:",
      "fstype": "NTFS",
      "total": 1000000000000,
      "used": 420000000000,
      "free": 580000000000,
      "used_percent": 42.0
    }
  ],
  "net_bytes_sent": 1450239044,
  "net_bytes_recv": 9823485023,
  "net_upload_rate": 125430.5,
  "net_download_rate": 845210.2,
  "top_processes": [
    {
      "pid": 28224,
      "name": "goland64.exe",
      "cpu_percent": 24.5,
      "memory_percent": 11.2,
      "memory_rss": 3824000000,
      "status": "running"
    },
    {
      "pid": 16984,
      "name": "chrome.exe",
      "cpu_percent": 8.1,
      "memory_percent": 3.4,
      "memory_rss": 1162000000,
      "status": "running"
    }
  ]
}
```

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
  "timestamp": "2026-10-30T20:30:02Z",
  "host_info": {
    "hostname": "DINH-PHAM",
    "os": "windows",
    "cpu_model": "Intel(R) Core(TM) i7-14700K",
    "cpu_physical_cores": 20,
    "cpu_logical_cores": 28,
    "cpu_cores": 28,
    "cpu_mhz": 3400.0,
    "cpu_speed_ghz": 3.4,
    "uptime": 15831
  },
  "cpu_percent": 15.2,
  "cpu_cores_percent": [12.0, 14.5, 8.0, 22.1],
  "mem_total": 34185805824,
  "mem_used": 25639354368,
  "mem_free": 8546451456,
  "mem_used_percent": 75.0,
  "swap_total": 4294967296,
  "swap_used": 1073741824,
  "swap_percent": 25.0,
  "partitions": [ ... ],
  "net_bytes_sent": 1450250000,
  "net_bytes_recv": 9823500000,
  "net_upload_rate": 5500.0,
  "net_download_rate": 7500.0,
  "top_processes": [ ... ]
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

// Data model cho HostInfo (bao gồm đầy đủ số nhân, luồng, xung nhịp)
export interface HostInfo {
  hostname: string;
  os: string;
  platform: string;
  platform_version: string;
  kernel_version: string;
  cpu_model: string;
  cpu_physical_cores: number; // Số nhân vật lý (VD: 20)
  cpu_logical_cores: number;  // Số luồng logic (VD: 28)
  cpu_cores: number;
  cpu_mhz: number;            // Tốc độ MHz (VD: 3400.0)
  cpu_speed_ghz: number;      // Tốc độ GHz (VD: 3.4)
  uptime: number;
}

// 1. Đăng nhập
export async function login(username: string, password: string) {
  const res = await axios.post(`${API_BASE}/auth/login`, { username, password });
  authToken = res.data.token;
  localStorage.setItem('sysdash_token', authToken);
  return res.data;
}

// 2. Lấy thông tin cấu hình CPU & Hệ điều hành
export async function getSystemInfo(): Promise<HostInfo> {
  const res = await axios.get(`${API_BASE}/system/info`, {
    headers: { Authorization: `Bearer ${authToken}` }
  });
  return res.data;
}

// 3. Lấy dữ liệu lịch sử vẽ biểu đồ (Chart.js / ECharts / Recharts)
export async function getMetricHistory(range: string = '24h', bucket: string = '15m') {
  const res = await axios.get(`${API_BASE}/metrics/history`, {
    headers: { Authorization: `Bearer ${authToken}` },
    params: { range, bucket }
  });
  return res.data.metrics;
}

// 4. Kết nối WebSocket thời gian thực
export function connectRealtimeStream(onData: (metric: any) => void) {
  const ws = new WebSocket(`ws://localhost:8095/api/v1/ws/metrics?token=${authToken}`);

  ws.onmessage = (event) => {
    const metric = JSON.parse(event.data);
    onData(metric);
  };

  ws.onerror = (err) => console.error('WebSocket Error:', err);
  ws.onclose = () => console.log('WebSocket Closed');

  return ws;
}
```

---

### 📱 5.2. Android Kotlin (Retrofit + OkHttp)

#### Data Model Kotlin:
```kotlin
data class HostInfo(
    val hostname: String,
    val os: String,
    val platform: String,
    val cpu_model: String,
    val cpu_physical_cores: Int, // Số nhân vật lý
    val cpu_logical_cores: Int,  // Số luồng logic
    val cpu_cores: Int,
    val cpu_mhz: Double,         // Tốc độ MHz (VD: 3400.0)
    val cpu_speed_ghz: Double,   // Tốc độ GHz (VD: 3.4)
    val uptime: Long
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
    .url("ws://10.0.2.2:8095/api/v1/ws/metrics?token=$jwtToken")
    .build()

val listener = object : WebSocketListener() {
    override fun onMessage(webSocket: WebSocket, text: String) {
        // Parse JSON RealtimeMetric và cập nhật UI (ProgressBar, Gauges, v.v.)
        val metric = Gson().fromJson(text, RealtimeMetric::class.java)
        runOnUiThread {
            updateDashboard(metric)
        }
    }
}

val ws = client.newWebSocket(request, listener)
```
