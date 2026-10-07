import Foundation

/// Finds the file descriptor of the utun interface this packet tunnel owns,
/// so the Go engine can read and write packets directly (much faster than
/// packetFlow). It scans open descriptors for the kernel-control socket
/// "com.apple.net.utun_control", as WireGuard for iOS does.
enum UtunFD {
    static func find() -> Int32? {
        var info = am_ctl_info()
        withUnsafeMutableBytes(of: &info.ctl_name) { buf in
            let name = Array("com.apple.net.utun_control".utf8)
            for (i, b) in name.enumerated() where i < buf.count - 1 { buf[i] = b }
        }
        for fd: Int32 in 0...1024 {
            var addr = am_sockaddr_ctl()
            var len = socklen_t(MemoryLayout<am_sockaddr_ctl>.size)
            let ret = withUnsafeMutablePointer(to: &addr) {
                $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { getpeername(fd, $0, &len) }
            }
            if ret != 0 || addr.sc_family != UInt8(AF_SYSTEM) { continue }
            if info.ctl_id == 0, ioctl(fd, AM_CTLIOCGINFO, &info) != 0 { continue }
            if addr.sc_id == info.ctl_id { return fd }
        }
        return nil
    }
}
