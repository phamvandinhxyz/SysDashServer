# Stage 1: Build ứng dụng
FROM golang:alpine AS builder

# Cài đặt git để clone repository
RUN apk add --no-cache git

# Thiết lập thư mục làm việc
WORKDIR /app

ARG CACHE_BUST=1

# Clone dự án từ GitHub
RUN git clone https://github.com/phamvandinhxyz/SysDashServer.git .

# Tải các module phụ thuộc
RUN go mod download

# Build mã nguồn Go thành file thực thi độc lập
RUN CGO_ENABLED=0 GOOS=linux go build -o sysdash-server .

# Stage 2: Môi trường chạy (Runner)
FROM alpine:latest

# Cài đặt chứng chỉ mạng cơ bản
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy file thực thi từ Stage 1 sang Stage 2
COPY --from=builder /app/sysdash-server .

# Khai báo port (Thay đổi 8080 thành port thực tế mà mã nguồn của bạn đang dùng)
EXPOSE 8095

# Lệnh khởi chạy server
CMD ["./sysdash-server"]