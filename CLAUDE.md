# Hytale server runner

This is ealy W.I.P of runner for Hytale servers that will use OCI (oras) as data store instead of relying on filesystem for state persistance.

Program Flow:
Load game server data from OCI registry
Run game server
Store gama server back to OCI registry

How write code here:
1. Avoid meangingless comments in codebase
2. Keep things DRY (Don't repeat your self)
3. Document everything with proper test suite focus on both happy and unhappy paths
