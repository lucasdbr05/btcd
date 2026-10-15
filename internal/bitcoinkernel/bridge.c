//go:build libbitcoinkernel && cgo

#include "bridge.h"

#include <stdlib.h>
#include <string.h>

typedef struct {
    uintptr_t handle;
} btcd_kernel_callback_data;

extern void goKernelDestroyHandle(uintptr_t handle);
extern void goKernelBlockChecked(
    uintptr_t handle, btck_Block* block,
    const btck_BlockValidationState* state);
extern void goKernelBlockConnected(
    uintptr_t handle, btck_Block* block,
    const btck_BlockTreeEntry* entry);
extern void goKernelBlockDisconnected(
    uintptr_t handle, btck_Block* block,
    const btck_BlockTreeEntry* entry);
extern void goKernelBlockTip(
    uintptr_t handle, btck_SynchronizationState state,
    const btck_BlockTreeEntry* entry, double verification_progress);
extern void goKernelFlushError(
    uintptr_t handle, const char* message, size_t message_len);
extern void goKernelFatalError(
    uintptr_t handle, const char* message, size_t message_len);

static btcd_kernel_callback_data* make_callback_data(uintptr_t handle)
{
    btcd_kernel_callback_data* data = calloc(1, sizeof(*data));
    if (data != NULL) data->handle = handle;
    return data;
}

static void destroy_callback_data(void* user_data)
{
    btcd_kernel_callback_data* data = user_data;
    if (data == NULL) return;

    goKernelDestroyHandle(data->handle);
    free(data);
}

static void block_checked(
    void* user_data, btck_Block* block,
    const btck_BlockValidationState* state)
{
    btcd_kernel_callback_data* data = user_data;
    goKernelBlockChecked(data->handle, block, state);
}

static void block_connected(
    void* user_data, btck_Block* block,
    const btck_BlockTreeEntry* entry)
{
    btcd_kernel_callback_data* data = user_data;
    goKernelBlockConnected(data->handle, block, entry);
}

static void block_disconnected(
    void* user_data, btck_Block* block,
    const btck_BlockTreeEntry* entry)
{
    btcd_kernel_callback_data* data = user_data;
    goKernelBlockDisconnected(data->handle, block, entry);
}

static void block_tip(
    void* user_data, btck_SynchronizationState state,
    const btck_BlockTreeEntry* entry, double verification_progress)
{
    btcd_kernel_callback_data* data = user_data;
    goKernelBlockTip(data->handle, state, entry, verification_progress);
}

static void flush_error(
    void* user_data, const char* message, size_t message_len)
{
    btcd_kernel_callback_data* data = user_data;
    goKernelFlushError(data->handle, message, message_len);
}

static void fatal_error(
    void* user_data, const char* message, size_t message_len)
{
    btcd_kernel_callback_data* data = user_data;
    goKernelFatalError(data->handle, message, message_len);
}

int btcd_kernel_make_validation_callbacks(
    uintptr_t handle, btck_ValidationInterfaceCallbacks* callbacks)
{
    memset(callbacks, 0, sizeof(*callbacks));

    btcd_kernel_callback_data* data = make_callback_data(handle);
    if (data == NULL) return 0;

    callbacks->user_data = data;
    callbacks->user_data_destroy = destroy_callback_data;
    callbacks->block_checked = block_checked;
    callbacks->block_connected = block_connected;
    callbacks->block_disconnected = block_disconnected;
    return 1;
}

int btcd_kernel_make_notification_callbacks(
    uintptr_t handle, btck_NotificationInterfaceCallbacks* callbacks)
{
    memset(callbacks, 0, sizeof(*callbacks));

    btcd_kernel_callback_data* data = make_callback_data(handle);
    if (data == NULL) return 0;

    callbacks->user_data = data;
    callbacks->user_data_destroy = destroy_callback_data;
    callbacks->block_tip = block_tip;
    callbacks->flush_error = flush_error;
    callbacks->fatal_error = fatal_error;
    return 1;
}
