# Network Router Daemon

Service tự động quản lý định tuyến mạng trên macOS, tối ưu hóa kết nối khi sử dụng đồng thời Wifi (cho nội bộ/công ty) và USB Tethering/Ethernet (cho Internet tốc độ cao).

Dự án này đã chuyển từ công cụ CLI đơn thuần sang kiến trúc **System Daemon + CLI Client + System Tray**, giúp nó chạy ngầm tự động và ổn định mà không cần người dùng can thiệp thủ công mỗi lần khởi động máy.

## ⚡ Quick Start - Cài đặt chỉ với 1 lệnh

```bash
git clone https://github.com/bez/network-router.git && cd network-router && sudo make install
```

Sau khi cài đặt xong, tray icon sẽ tự động xuất hiện trên menu bar! 🎉

## Tính năng

*   **Background Daemon**: Chạy ngầm như một system service (root), tự động khởi động cùng macOS.
*   **System Tray Icon**: Giao diện đồ họa trên menu bar để kiểm soát dễ dàng, không cần terminal.
*   **Auto-Switching**: Tự động phát hiện khi bạn cắm/rút iPhone USB Tethering hoặc kết nối Wifi để điều chỉnh bảng định tuyến (Routing Table) ngay lập tức.
*   **Split Tunneling**:
    *   **Internet**: Định tuyến traffic ra ngoài qua đường truyền nhanh nhất (ví dụ: USB Tethering).
    *   **Internal**: Định tuyến các domain nội bộ (`*.corp.com`), IP private (`192.168.x.x`) đi qua Wifi.
*   **CLI Client**: Giao diện dòng lệnh để kiểm tra trạng thái hoặc bật/tắt service dễ dàng.
*   **Auto-disable on Clear**: Khi xóa routes thì tự động tắt auto-routing để tránh bị apply lại.

## Kiến trúc

*   **Daemon (`network-router daemon`)**:
    *   Chạy dưới quyền **root**.
    *   Lắng nghe thay đổi mạng (Network Monitor).
    *   Thực thi lệnh `route` và `networksetup`.
    *   Mở socket IPC tại `/tmp/network-router.sock` để nhận lệnh điều khiển.
*   **Tray App (`network-router tray`)**:
    *   Chạy với quyền **user**.
    *   Hiển thị icon trên menu bar.
    *   Giao tiếp với Daemon qua Unix socket.
    *   Menu điều khiển: Enable/Disable, Apply, Clear, Status.
*   **Client (`network-router <cmd>`)**:
    *   Người dùng chạy lệnh để giao tiếp với Daemon qua socket.

## 📦 Cài đặt

### Cách 1: Cài đặt tự động (Khuyến nghị)

Chỉ cần chạy một lệnh duy nhất:

```bash
git clone https://github.com/bez/network-router.git && cd network-router && sudo make install
```

Script sẽ tự động:
- ✅ Build binary từ source code
- ✅ Cài đặt vào `/usr/local/bin/network-router`
- ✅ Cài đặt config file vào `/usr/local/etc/network-router/`
- ✅ Đăng ký và khởi động daemon service (LaunchDaemon)
- ✅ Đăng ký và khởi động tray app (LaunchAgent)
- ✅ Chạy health check để kiểm tra service
- ✅ Hiển thị tray icon trên menu bar

### Cách 2: Cài đặt thủ công (Từng bước)

Nếu muốn kiểm soát chi tiết hơn:

```bash
# Clone repository
git clone https://github.com/bez/network-router.git
cd network-router

# Build binary
make build

# Install service (yêu cầu password sudo)
sudo ./install_service.sh
```

Script cài đặt sẽ:
1.  Build binary `network-router` và copy vào `/usr/local/bin/`.
2.  Tạo thư mục cấu hình tại `/usr/local/etc/network-router/`.
3.  Cài đặt daemon service vào `/Library/LaunchDaemons/`.
4.  **Cài đặt tray app tự động chạy khi login** vào `~/Library/LaunchAgents/`.
5.  Khởi động cả daemon và tray app ngay lập tức.

**Lưu ý:** Tray icon sẽ tự động xuất hiện trên menu bar sau khi cài đặt xong! 🎉

### 2. Kiểm tra hoạt động

Sau khi cài đặt xong, kiểm tra trạng thái service:

```bash
network-router status
```

Nếu cài đặt thành công, bạn sẽ thấy trạng thái **"Running"** và thông tin về kết nối mạng hiện tại.

## Cấu hình

File cấu hình được đặt tại:  
**`/usr/local/etc/network-router/config.yaml`**

Bạn có thể chỉnh sửa file này để thêm domain hoặc dải IP công ty:

```yaml
# Các domain nội bộ cần đi qua Wifi
internal_domains:
  - "*.internal.company.com"
  - "gitlab.local"

# Các dải IP nội bộ cần đi qua Wifi
internal_cidrs:
  - "192.168.1.0/24"
  - "10.0.0.0/8"

# Tên interface (thường không cần sửa nếu dùng tiếng Anh)
wifi_interface_name: "Wi-Fi"
phone_interface_name: "iPhone USB"
```

**Lưu ý:** Sau khi sửa file cấu hình, bạn cần restart service để áp dụng:

```bash
network-router restart
```

## Hướng dẫn Sử dụng

### System Tray (Mặc định)

Sau khi cài đặt, **tray icon tự động xuất hiện** trên menu bar mỗi khi bạn login. Không cần chạy lệnh thủ công!

**Trạng thái icon:**
*   🟢 **Icon màu xanh**: Daemon hoạt động và routes đã được apply.
*   ⚫ **Icon màu xám**: Daemon chạy nhưng routes chưa apply hoặc auto-routing bị tắt.
*   🔴 **Icon màu đỏ**: Không kết nối được với daemon.

**Menu controls:**
*   **Status Display**: Hiển thị trạng thái hiện tại (read-only).
*   **Enable/Disable Auto-Routing**: Bật/tắt tính năng tự động.
*   **Apply Routes**: Ép buộc áp dụng routes ngay.
*   **Clear Routes**: Xóa tất cả routes.
*   **Hide Icon**: Ẩn icon khỏi menu bar (vẫn chạy ngầm).
*   **Quit**: Tắt tray app (có thể khởi động lại bằng lệnh `network-router tray`).

**Quản lý tray app:**
```bash
# Kiểm tra trạng thái
launchctl list | grep network-router.tray

# Tắt tray app (tạm thời)
launchctl unload ~/Library/LaunchAgents/com.bez.network-router.tray.plist

# Bật lại
launchctl load ~/Library/LaunchAgents/com.bez.network-router.tray.plist

# Hoặc chạy thủ công
network-router tray
```

### CLI Commands

Nếu không muốn dùng tray, bạn vẫn có thể điều khiển qua terminal. Lưu ý: các lệnh CLI **không cần sudo** nữa (trừ khi socket bị lỗi permissions).

#### Xem trạng thái
Kiểm tra xem daemon có đang chạy không và mạng nào đang active.
```bash
network-router status
```

#### Bật/Tắt tính năng tự động (Enable/Disable)
Tạm dừng tính năng tự động định tuyến (vẫn giữ service chạy nhưng không can thiệp mạng).
```bash
network-router disable
network-router enable
```

#### Áp dụng thủ công (Apply/Clear)
Ép buộc chạy logic định tuyến ngay lập tức (hữu ích khi muốn test config mới mà không chờ auto-detect).
```bash
network-router apply   # Thêm routes
network-router clear   # Xóa routes, trả về mặc định
```

## Các Tình Huống Thường Gặp

### Scenario 1: Làm việc từ nhà
1. Khởi động tray app: `network-router tray`
2. Kết nối iPhone USB để có Internet tốc độ cao
3. Kết nối WiFi công ty để truy cập mạng nội bộ
4. ✅ Routes tự động được áp dụng, traffic tách biệt

### Scenario 2: Quán cà phê (không có WiFi công ty)
1. Chỉ kết nối WiFi quán
2. Routes không được áp dụng (không match WiFi name)
3. Traffic đi qua default gateway bình thường

### Scenario 3: Văn phòng (chỉ WiFi)
1. Ngắt iPhone USB
2. Routes tự động được xóa
3. Toàn bộ traffic đi qua WiFi

## Development

### Build & Test

```bash
# Build project
make build

# Chạy daemon ở foreground (để debug)
make daemon

# Chạy tray app locally
make tray

# Test icon generation
make test-icons

# Clean build artifacts
make clear-build
```

### Testing Tray App

1. Đảm bảo daemon đang chạy:
   ```bash
   sudo make daemon
   ```

2. Chạy tray app ở terminal khác:
   ```bash
   make tray
   ```

3. Kiểm tra:
   - Icon xuất hiện trên menu bar
   - Click vào icon để xem menu
   - Thử các chức năng Enable/Disable, Apply, Clear
   - Verify icon color thay đổi theo trạng thái

### Technical Details

**Tray Implementation:**
- Library: `github.com/getlantern/systray`
- Polling interval: 5 giây
- Icon generation: Dynamic PNG (32x32 pixels)
- IPC Protocol: JSON qua Unix socket

**Socket Configuration:**
- Path: `/tmp/network-router.sock`
- Permissions: `0666` (cho phép user-mode tray app kết nối với root daemon)
- Timeout: 5 giây

**Security Notes:**
- Socket `0666` cho phép mọi local user kết nối
- Phù hợp cho single-user macOS system
- Nếu cần security cao hơn: dùng `0660` + group ownership hoặc thêm authentication

## Tips & Best Practices

1. **Performance**: Routes được cache, chỉ reapply khi có network change
2. **Battery**: Minimal impact (~5-10MB RAM, negligible CPU)
3. **Updates**: Pull code mới, rebuild và restart service
4. **Config Backup**: Lưu backup của `/usr/local/etc/network-router/config.yaml`
5. **Multiple Macs**: Cùng một config hoạt động nếu dùng cùng tên network

## Troubleshooting

### Daemon không chạy

```bash
# Kiểm tra launchd status
sudo launchctl list | grep network-router

# Nếu không thấy, load lại thủ công
sudo launchctl load /Library/LaunchDaemons/com.bez.network-router.plist
```

### Tray app không kết nối được

```bash
# Kiểm tra socket tồn tại và có đúng permissions
ls -la /tmp/network-router.sock
# Cần thấy: srw-rw-rw- (0666 permissions)

# Nếu daemon đang chạy nhưng socket không tồn tại, restart daemon
sudo launchctl unload /Library/LaunchDaemons/com.bez.network-router.plist
sudo launchctl load /Library/LaunchDaemons/com.bez.network-router.plist
```

### Routes không được apply

```bash
# Kiểm tra tên interfaces có đúng không
networksetup -listallhardwareports

# Xem routing table hiện tại
netstat -rn

# Thử apply thủ công để xem error message
network-router apply

# Xem daemon logs
tail -f /var/log/network-router.log
```

### Xem Logs

```bash
# Live daemon logs
tail -f /var/log/network-router.log

# System logs
sudo grep "network-router" /var/log/system.log

# Nếu log file không tồn tại, tạo mới
sudo touch /var/log/network-router.log
sudo chmod 644 /var/log/network-router.log
```

## Gỡ cài đặt (Uninstall)

Để xóa hoàn toàn service khỏi máy:

```bash
# Chạy uninstall script
sudo ./uninstall_service.sh
```

Hoặc xóa thủ công:

```bash
# Stop daemon và tray app
sudo launchctl unload /Library/LaunchDaemons/com.bez.network-router.plist
launchctl unload ~/Library/LaunchAgents/com.bez.network-router.tray.plist
killall network-router 2>/dev/null || true

# Remove files
sudo rm /Library/LaunchDaemons/com.bez.network-router.plist
rm ~/Library/LaunchAgents/com.bez.network-router.tray.plist
sudo rm /usr/local/bin/network-router
sudo rm -rf /usr/local/etc/network-router
sudo rm /tmp/network-router.sock 2>/dev/null || true

echo "✅ Uninstall completed."
```

## Tài Liệu Bổ Sung

- **[ARCHITECTURE.md](ARCHITECTURE.md)**: Chi tiết về kiến trúc hệ thống và data flow
- **Issues**: https://github.com/bez/network-router/issues

## License

MIT License - Xem file LICENSE để biết chi tiết.
