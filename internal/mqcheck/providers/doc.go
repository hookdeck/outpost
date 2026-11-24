// Package providers holds one file per broker. Each file registers its
// provider with provider.Register from init(); importing this package (as
// cmd/mqcheck does) makes all of them available. Adding a broker is adding a
// file here: see cmd/mqcheck/ADDING_A_PROVIDER.md.
package providers
