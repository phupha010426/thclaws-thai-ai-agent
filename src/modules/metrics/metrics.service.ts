/**
 * Minimal in-process counters + histograms. Exposed at GET /metrics in
 * Prometheus text format so it can be scraped without pulling in a full
 * client library. Resets on restart — good enough for ops visibility while
 * we keep the stack small.
 */
const counters = new Map<string, number>();
const histograms = new Map<string, number[]>();

export const metrics = {
  inc(name: string, by = 1) {
    counters.set(name, (counters.get(name) ?? 0) + by);
  },
  observe(name: string, value: number) {
    const bucket = histograms.get(name) ?? [];
    bucket.push(value);
    if (bucket.length > 1024) bucket.shift();
    histograms.set(name, bucket);
  },
  render(): string {
    const lines: string[] = [];
    for (const [name, value] of counters) {
      lines.push(`# TYPE ${name} counter`);
      lines.push(`${name} ${value}`);
    }
    for (const [name, values] of histograms) {
      if (values.length === 0) continue;
      const sorted = [...values].sort((a, b) => a - b);
      const p50 = sorted[Math.floor(sorted.length * 0.5)];
      const p95 = sorted[Math.floor(sorted.length * 0.95)];
      const p99 = sorted[Math.floor(sorted.length * 0.99)];
      lines.push(`# TYPE ${name} summary`);
      lines.push(`${name}{quantile="0.5"} ${p50}`);
      lines.push(`${name}{quantile="0.95"} ${p95}`);
      lines.push(`${name}{quantile="0.99"} ${p99}`);
      lines.push(`${name}_count ${values.length}`);
    }
    return lines.join('\n') + '\n';
  }
};
