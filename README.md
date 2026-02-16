# Operator Controller Shell

Experimental read-eval-print loop shell for the [operator-controller](https://github.com/operator-framework/operator-controller). The goal is to make interacting with ClusterExtensions and ClusterCatalogs more ergonomic.

## Building
```sh
go build -o opconsh .
```

## Using
`opconsh` connects to your cluster with the default kubeconfig, or you can pass the `--kubeconfig` argument for custom locations.

You will enter a shell-like environment with tab-completion and command history. Try `help` to see a list of commands you can run in the "root" directory.

The most feature-complete commands relate to interacting with ClusterCatalogs. Try using `enter` to enter a ClusterCatalog context where you can list the available packages, read the rendered Markdown descriptions of packages, list their channels and versions, and more.

Upon entering a ClusterCatalog, `opconsh` will automatically set up port-forwarding to the `catalogd` API endpoint and populate a local cache of the available operators.
