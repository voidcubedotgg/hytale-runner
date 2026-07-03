### Must to have
- Rewrite into cobra + viper (done)
- Use proper logger (uber/zap) (done)
- ExtraJVMArgs - allow specification of JVM args (done)
- ExtraServer - allow adding extra server args (done)
- README.md (done)
- CI/CD (goreleaser etc.) (done)

### Nice to have 
- Token broker?
- Input named pipe (console commands)

### AWS MicroVM runtime
- Lifecycle hook server (`serve`): run/suspend/resume/terminate + /ready + /health (done)
- Pull state on run, push on terminate (done)
- Dockerfile on Lambda AL2023 base + JRE (done)
- Live/suspend/periodic push + save-all console channel (needs input named pipe)
- Hytale readiness detection so /run returns 200 only once the world is serving
- Bake booted server into snapshot + live world-reload (near-instant launch)
- Multi-tenant runHookPayload schema (override state repo/tag per MicroVM)