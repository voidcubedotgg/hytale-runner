### Must to have
- Rewrite into cobra + viper (done)
- Use proper logger (uber/zap) (done)
- ExtraJVMArgs - allow specification of JVM args (done)
- ExtraServer - allow adding extra server args (done)
- README.md (done)
- CI/CD (goreleaser etc.) (done)

### Nice to have 
- NATS status KV (done)
- NATS as log transport (storage stays elsewhere, e.g. Loki)
- Console commands over NATS request-reply (replaces input named pipe idea)
- Token broker?
- Readyz and Livez
- Dockerfile for production