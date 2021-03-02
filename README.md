# Helm Optimize Resource Plugin

## Introduction
This is a helmV3 plugin to optimize your container resources (CPU/Memory Requests/Limits) by injecting optimal specifications, extracted from your choice of a parameter repository whenever helm is called to install or upgrade a chart.

## Pre-requisits
- Densify account, which is provided with a Densify subscription or through a free trial (https://www.densify.com/service/signup)
- Linux/Windows client machine with kubectl and helm installed and configured

## Usage
Once installed, the plugin is made available through the 'optimize' keyword which is passed in as the first parameter to helm.  Here is an output of the helm command after the plugin is installed.
```
Available Commands:
  completion  generate autocompletion scripts for the specified shell
  create      create a new chart with the given name
  dependency  manage a chart's dependencies
  env         helm client environment information
  get         download extended information of a named release
  help        Help about any command
  history     fetch release history
  install     install a chart
  lint        examine a chart for possible issues
  list        list releases```
  optimize    optimize resource spec of running containers during an install or upgrade
  package     package a chart directory into a chart archive
  plugin      install, list, or uninstall Helm plugins
  pull        download a chart from a repository and (optionally) unpack it in local directory
  repo        add, list, remove, update, and index chart repositories
  rollback    roll back a release to a previous revision
  search      search for a keyword in charts
  show        show information of a chart
  status      display the status of the named release
  template    locally render templates
  test        run tests for a release
  uninstall   uninstall a release
  upgrade     upgrade a release
  verify      verify that a chart at the given path has been signed and is valid
  version     print the client version information
  




Simply use helm as you normally would, but add the 'optimize' keyword before any command.  The plugin will lookup the optimal resource spec from the configured repository.
