# Claude Development Guidelines for opconsh

This file contains requirements and guidelines for Claude when working on the opconsh CLI project.

opconsh is an experimental read-eval-print loop shell for the [operator-controller](https://github.com/operator-framework/operator-controller). The goal is to make interacting with ClusterExtensions and ClusterCatalogs more ergonomic.

## Shell-like design

opconsh should abide by familiar REPL and shell conventions. In contrast to the existing tools for interacting with kubernetes clusters like kubectl, opconsh should be interactive and ergonomic, making the experience of working with your cluster more like interacting with your personal machine's command line. The goal is for users to be able to quickly and efficiently understand the state of their cluster's extensions and catalogs without getting hung up on dynamic kubernetes resource names, piping yaml between kubectl commands, or other existing methods of interacting with a cluser on the CLI.

## Autocomplete Requirements

### Core Principle
All commands in opconsh MUST provide intelligent, cluster-aware autocomplete that suggests appropriate resource names from the connected Kubernetes cluster based on the command context.

### Mandatory Coverage
1. **All Command Trees**: Every command and subcommand must have appropriate completion
2. **Resource Names**: All resource-specific arguments must complete with actual cluster resource names
3. **Context Sensitivity**: Completion suggestions must be contextually appropriate for the command being typed
4. **Performance**: Completions must be cached (30s TTL) to avoid excessive API calls during typing

### Example Completion Patterns

#### Top-level Commands
- `enter <tab>` → ClusterCatalog names
- `diagnose catalog <tab>` → ClusterCatalog names  
- `diagnose extension <tab>` → ClusterExtension names
- `catalogs get <tab>` → ClusterCatalog names
- `extensions get <tab>` → ClusterExtension names

#### Nested Resources
- `catalogs package <catalog> <tab>` → Package names from specified catalog
- `catalogs search <catalog> <tab>` → No completion (search term is user input)

#### Catalog Context
- `get <tab>` → Package names within current catalog
- `describe <tab>` → Package names within current catalog
- `channels <tab>` → Package names within current catalog

### Implementation Requirements

#### Cache Integration
- All completers MUST use the existing cache system (`pkg/repl/cache.go`)
- Cache misses should gracefully handle API failures (return empty list, don't block)
- Cache should be invalidated on `refresh` command

#### Error Handling
- Network failures during completion should not interrupt user experience
- Invalid cluster state should result in empty completion (not errors)
- Completion functions must never panic or cause CLI crashes

#### Consistency Standards
- All completion functions should follow naming pattern: `<resourceType>NamesCompleter`
- Dynamic completers must use `readline.PcItemDynamic()` wrapper
- Static command completers use `readline.PcItem()`

#### Future Extension Points
- System should easily support new resource types (e.g., bundle versions, channel names)
- Completion tree structure should be maintainable as new commands are added
- Multi-level completions should support arbitrary nesting depth

### Testing Considerations
- Completion should work in both interactive mode and with partial command prefixes
- Performance testing with large numbers of resources (100+ catalogs, 1000+ packages)
- Graceful degradation when cluster is unreachable or slow

## Code Quality Standards

### Error Handling
- Never surface prescriptive troubleshooting suggestions to users
- Focus on surfacing actual error conditions, events, and status information
- Let users interpret diagnostic information rather than providing recommendations

### User Interface
- Use simple ASCII characters for status indicators (`[+]`, `[!]`, `[?]`) instead of Unicode symbols
- Avoid terminal control sequences that might interfere with command output
- Keep command output clean and parseable

### Performance
- Cache expensive API operations (ClusterCatalog lists, package queries) with appropriate TTL
- Gracefully handle network failures and cluster connectivity issues
- Provide responsive autocomplete even with large resource counts

When adding new commands, ensure they follow the established patterns for completion, caching, and error handling.
