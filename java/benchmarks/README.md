# Java SDK Benchmarks

Run from `sdks/java`:

```bash
./gradlew :benchmarks:jmh
```

Benchmarks included:

- `mapOpenAiSync`: provider mapper throughput for sync payload mapping.
- `recordGenerationHotPath`: enqueue/record runtime hot path throughput.

Baseline results are written to `benchmarks/build/reports/jmh/results.json`.
