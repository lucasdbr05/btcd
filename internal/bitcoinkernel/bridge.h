//go:build libbitcoinkernel && cgo

#ifndef BTCD_BITCOINKERNEL_BRIDGE_H
#define BTCD_BITCOINKERNEL_BRIDGE_H

#include <bitcoinkernel.h>
#include <stdint.h>

int btcd_kernel_make_validation_callbacks(
    uintptr_t handle, btck_ValidationInterfaceCallbacks* callbacks);

int btcd_kernel_make_notification_callbacks(
    uintptr_t handle, btck_NotificationInterfaceCallbacks* callbacks);

#endif // BTCD_BITCOINKERNEL_BRIDGE_H
