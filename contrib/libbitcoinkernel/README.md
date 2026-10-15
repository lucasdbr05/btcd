# libbitcoinkernel development dependency

This directory pins and documents the native `libbitcoinkernel` dependency
used by the experimental btcd Go binding.  It does not enable
`libbitcoinkernel` in the regular btcd build.

`libbitcoinkernel` is experimental.  Its C API is unversioned and can change
between Bitcoin Core commits.  Always build the revision recorded in
[`VERSION`](VERSION); do not build an arbitrary `master` checkout.

## Initial platform scope

The first binding iteration targets:

- macOS on arm64 for local development;
- Linux on amd64 for continuous integration;
- regtest for the initial lifecycle and block-processing tests.

Other platforms and networks can be enabled after the binding lifecycle and
callback behavior are covered by tests.  Bitcoin Core's simnet equivalent is
not exposed by the current C API.

## Prerequisites

Install Bitcoin Core's documented Unix build dependencies, a C/C++ compiler,
CMake, and `pkg-config`.  See Bitcoin Core's build documentation for the exact
packages required by the host operating system:

<https://github.com/bitcoin/bitcoin/tree/cce32b16874e85e8a00cfa1a2190f0b6f4dcaf66/doc#building>

The commands below use a separate source checkout and installation prefix so
that the dependency does not write generated files into the btcd repository.

## Build the pinned revision

Read the pinned commit from `VERSION`, clone Bitcoin Core, and check out that
exact revision:

```sh
kernel_commit=$(sed -n 's/^BITCOIN_CORE_COMMIT=//p' contrib/libbitcoinkernel/VERSION)
git clone https://github.com/bitcoin/bitcoin.git bitcoin-core
git -C bitcoin-core checkout --detach "$kernel_commit"
```

Configure a minimal shared-library build.  Replace `$kernel_prefix` with an
absolute installation directory:

```sh
kernel_prefix="$PWD/bitcoin-kernel-install"

cmake -S bitcoin-core -B bitcoin-core/build-kernel \
  -DCMAKE_INSTALL_PREFIX="$kernel_prefix" \
  -DCMAKE_BUILD_TYPE=RelWithDebInfo \
  -DBUILD_SHARED_LIBS=ON \
  -DBUILD_BITCOIN_BIN=OFF \
  -DBUILD_KERNEL_LIB=ON \
  -DBUILD_KERNEL_TEST=OFF \
  -DBUILD_UTIL_CHAINSTATE=OFF \
  -DBUILD_BENCH=OFF \
  -DBUILD_CLI=OFF \
  -DBUILD_DAEMON=OFF \
  -DBUILD_FUZZ_BINARY=OFF \
  -DBUILD_GUI=OFF \
  -DBUILD_GUI_TESTS=OFF \
  -DBUILD_TESTS=OFF \
  -DBUILD_TX=OFF \
  -DBUILD_UTIL=OFF \
  -DBUILD_WALLET_TOOL=OFF \
  -DENABLE_IPC=OFF \
  -DENABLE_WALLET=OFF

cmake --build bitcoin-core/build-kernel --target libbitcoinkernel --parallel
cmake --install bitcoin-core/build-kernel \
  --component libbitcoinkernel
```

`CMAKE_INSTALL_PREFIX` must be set while configuring.  The generated
`libbitcoinkernel.pc` records that configured prefix, so changing only the
install command's `--prefix` option would leave pkg-config pointing at the
wrong directories.

The installation should contain:

```text
<prefix>/include/bitcoinkernel.h
<prefix>/lib/libbitcoinkernel.{so,dylib}
<prefix>/lib/pkgconfig/libbitcoinkernel.pc
```

Some systems use `lib64` instead of `lib`; the installed pkg-config file
contains the authoritative paths.

## Verify the installation

Point `pkg-config` at the installation and run the verifier:

```sh
export PKG_CONFIG_PATH="$kernel_prefix/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
contrib/libbitcoinkernel/verify-install.sh
```

The verifier:

1. locates `libbitcoinkernel` through `pkg-config`;
2. checks the installed header against the pinned SHA-256;
3. compiles a C program using only pkg-config flags;
4. runs the program to create and destroy regtest chain parameters and a
   kernel context.

A header mismatch normally means that Bitcoin Core was built from a different
commit.  Upgrading the pinned revision requires reviewing the C API changes and
updating both values in `VERSION` together.

## Verify the Go lifecycle binding

The regular build uses a stub and does not require the native library:

```sh
go test ./internal/bitcoinkernel
```

Enable the native lifecycle binding explicitly with the build tag:

```sh
CGO_ENABLED=1 go test -tags=libbitcoinkernel ./internal/bitcoinkernel
```

The tagged test creates and destroys a kernel context and chainstate manager
for every supported Bitcoin network.  It also verifies callback handle
ownership and the manager's directory, worker, and cache configuration.
`PKG_CONFIG_PATH` and the platform runtime lookup variable must be configured
as described above.

## Runtime lookup

The verifier temporarily adds the pkg-config `libdir` to the platform's shared
library search path.  Applications and tests will need an equivalent runtime
configuration until packaging or rpath handling is added:

- Linux: `LD_LIBRARY_PATH`;
- macOS: `DYLD_LIBRARY_PATH`.

Static linking and distribution of native artifacts are intentionally outside
this initial dependency setup.
