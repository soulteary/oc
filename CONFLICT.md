# OC, mc and executable names

This repository builds and installs the executable as `oc`. It can coexist with the upstream MinIO Client executable `mc` and with Midnight Commander, which also uses the command name `mc`. OC does not require either program to be removed or renamed.

Use `oc --version` to identify the program you are running. A binary built in this checkout is invoked as `./oc` on Unix or `oc.exe` on Windows; putting a different executable earlier in `PATH` can otherwise select the wrong program.

The Go module path remains `github.com/soulteary/mc` for source compatibility. Existing copyright notices, attribution and some historical file conventions retain upstream names. These names do not change the executable name or make OC affiliated with or endorsed by MinIO, Inc.

OC uses its own default configuration directory: `~/.oc` on Unix and the user-directory `oc` folder on Windows. Renaming the binary does not change that directory. OC does not automatically load or modify an existing mc configuration; use the explicit import described in [migration](docs/migration.md).

Use the [installation guide](docs/installation.md) for current artifacts and container names, the [configuration guide](docs/configuration.md) for overrides, and the [contribution guide](CONTRIBUTING.md) when changing source or packaging. Package descriptions should identify this repository as OC and retain its license and upstream attribution.
