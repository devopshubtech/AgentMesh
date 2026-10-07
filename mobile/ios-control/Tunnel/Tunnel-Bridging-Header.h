// Kernel-control declarations that the iOS SDK does not export. They are
// needed to find the utun socket behind NEPacketTunnelProvider (same approach
// as WireGuard for iOS); values from Darwin's sys/kern_control.h.
#include <stdint.h>
#include <sys/types.h>
#include <sys/socket.h>
#include <sys/ioctl.h>

#ifndef AF_SYSTEM
#define AF_SYSTEM 32
#endif

#define AM_CTLIOCGINFO 0xc0644e03UL

struct am_ctl_info {
    u_int32_t ctl_id;
    char ctl_name[96];
};

struct am_sockaddr_ctl {
    u_char sc_len;
    u_char sc_family;
    u_int16_t ss_sysaddr;
    u_int32_t sc_id;
    u_int32_t sc_unit;
    u_int32_t sc_reserved[5];
};
