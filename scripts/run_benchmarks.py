#!/usr/bin/env python3
import argparse
import csv
import datetime as dt
import json
import os
import re
import subprocess
import sys
from pathlib import Path
from typing import Dict, List, Tuple

import yaml


def load_dotenv(path: Path) -> None:
    if not path.exists():
        return
    for line in path.read_text(encoding="utf-8").splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in stripped:
            continue
        key, value = stripped.split("=", 1)
        key = key.strip()
        value = value.strip().strip('"').strip("'")
        os.environ.setdefault(key, value)


def resolve_env(value: str) -> str:
    pattern = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}")

    def repl(match: re.Match[str]) -> str:
        key = match.group(1)
        return os.getenv(key, "")

    return pattern.sub(repl, value)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run gateway benchmarks with Locust")
    parser.add_argument("--config", default="benchmarks.yaml", help="Benchmark config file")
    parser.add_argument("--dotenv", default=".env", help="dotenv file path")
    parser.add_argument("--out-dir", default="results", help="Output directory")
    parser.add_argument(
        "--gateways",
        default="",
        help="Comma-separated gateway names to run (default: all in config)",
    )
    parser.add_argument(
        "--scenarios",
        default="",
        help="Comma-separated scenario names to run (default: all in config)",
    )
    parser.add_argument("--dry-run", action="store_true", help="Print commands without executing")
    parser.add_argument(
        "--repeat",
        type=int,
        default=1,
        help="Number of times to repeat each gateway/scenario pair (results are averaged)",
    )
    return parser.parse_args()


def load_config(path: Path) -> Dict:
    with path.open("r", encoding="utf-8") as file:
        cfg = yaml.safe_load(file)
    if not isinstance(cfg, dict):
        raise ValueError("Invalid config: expected mapping")
    return cfg


def parse_selection(value: str) -> List[str]:
    if not value.strip():
        return []
    return [item.strip() for item in value.split(",") if item.strip()]


def pick_gateways(cfg: Dict, selected: List[str]) -> Dict:
    gateways = cfg.get("gateways", {})
    if not isinstance(gateways, dict) or not gateways:
        raise ValueError("Config requires non-empty 'gateways'")
    if not selected:
        return gateways
    missing = [name for name in selected if name not in gateways]
    if missing:
        raise ValueError(f"Unknown gateways: {', '.join(missing)}")
    return {name: gateways[name] for name in selected}


def pick_scenarios(cfg: Dict, selected: List[str]) -> List[Dict]:
    scenarios = cfg.get("scenarios", [])
    if not isinstance(scenarios, list) or not scenarios:
        raise ValueError("Config requires non-empty 'scenarios'")
    scenario_map = {str(s.get("name")): s for s in scenarios}
    if not selected:
        return scenarios
    missing = [name for name in selected if name not in scenario_map]
    if missing:
        raise ValueError(f"Unknown scenarios: {', '.join(missing)}")
    return [scenario_map[name] for name in selected]


def run_locust(
    gateway_name: str,
    gateway_cfg: Dict,
    scenario: Dict,
    run_dir: Path,
    dry_run: bool,
    repeat_index: int = 0,
) -> Path:
    base_url = resolve_env(str(gateway_cfg.get("base_url", ""))).rstrip("/")
    if not base_url and dry_run:
        base_url = "http://example.invalid"
    if not base_url:
        raise ValueError(f"Gateway '{gateway_name}' is missing base_url")

    model = resolve_env(str(gateway_cfg.get("model", os.getenv("MODEL", "gpt-4o-mini"))))
    api_key = resolve_env(str(gateway_cfg.get("api_key", "")))
    request_path = str(gateway_cfg.get("request_path", "/v1/chat/completions"))
    extra_headers = gateway_cfg.get("extra_headers", {})

    scenario_name = str(scenario.get("name"))
    users = str(scenario.get("users", 10))
    spawn_rate = str(scenario.get("spawn_rate", 2))
    duration = str(scenario.get("duration", "2m"))
    prompt = str(scenario.get("prompt", "Return one short sentence."))
    max_tokens = str(scenario.get("max_tokens", 64))
    temperature = str(scenario.get("temperature", 0))
    stream = "true" if scenario.get("stream", False) else "false"
    wait_time = scenario.get("wait_time", [0, 0])  # [min_ms, max_ms]
    wait_min = str(wait_time[0]) if isinstance(wait_time, list) else "0"
    wait_max = str(wait_time[1]) if isinstance(wait_time, list) else "0"

    suffix = f"-run{repeat_index + 1}" if repeat_index > 0 else ""
    run_prefix = run_dir / f"{gateway_name}-{scenario_name}{suffix}"

    env = os.environ.copy()
    env["API_KEY"] = api_key
    env["MODEL"] = model
    env["REQUEST_PATH"] = request_path
    env["PROMPT"] = prompt
    env["MAX_TOKENS"] = max_tokens
    env["TEMPERATURE"] = temperature
    env["EXTRA_HEADERS"] = json.dumps(extra_headers)
    env["STREAM"] = stream
    env["WAIT_TIME_MIN"] = wait_min
    env["WAIT_TIME_MAX"] = wait_max

    cmd = [
        "locust",
        "-f",
        "locustfile.py",
        "--headless",
        "--host",
        base_url,
        "--users",
        users,
        "--spawn-rate",
        spawn_rate,
        "--run-time",
        duration,
        "--csv",
        str(run_prefix),
        "--csv-full-history",
    ]

    print(f"\n=== Running {gateway_name} / {scenario_name} ===")
    print(" ".join(cmd))

    if not dry_run:
        subprocess.run(cmd, check=True, env=env)

    return Path(f"{run_prefix}_stats.csv"), run_prefix


EXPECTED_COLUMNS = {
    "Request Count",
    "Failure Count",
    "Requests/s",
    "50%",
    "95%",
    "99%",
    "Average Response Time",
}


def read_locust_stats(stats_path: Path) -> Dict[str, str]:
    if not stats_path.exists():
        raise FileNotFoundError(f"Missing Locust stats file: {stats_path}")

    with stats_path.open("r", encoding="utf-8") as file:
        reader = csv.DictReader(file)
        rows = list(reader)
        fieldnames = set(reader.fieldnames or [])

    missing = EXPECTED_COLUMNS - fieldnames
    if missing:
        raise ValueError(
            f"Locust CSV {stats_path} is missing expected columns: {', '.join(sorted(missing))}. "
            "Check that your Locust version is compatible (tested with locust==2.37.x)."
        )

    target = None
    for row in rows:
        if row.get("Name") == "Aggregated":
            target = row
            break

    if target is None:
        for row in rows:
            if row.get("Name") in ("chat.completions", "chat.completions.stream"):
                target = row
                break

    if target is None:
        raise ValueError(f"Could not find aggregated row in {stats_path}")

    return {
        "requests": target.get("Request Count", "0"),
        "failures": target.get("Failure Count", "0"),
        "rps": target.get("Requests/s", "0"),
        "p50_ms": target.get("50%", "0"),
        "p95_ms": target.get("95%", "0"),
        "p99_ms": target.get("99%", "0"),
        "avg_ms": target.get("Average Response Time", "0"),
    }


def failure_pct(requests: str, failures: str) -> str:
    req = float(requests or 0)
    fail = float(failures or 0)
    if req <= 0:
        return "0.00"
    return f"{(fail / req) * 100:.2f}"


def average_stats(stats_list: List[Dict[str, str]]) -> Dict[str, str]:
    """Average numeric fields across repeat runs."""
    if len(stats_list) == 1:
        return stats_list[0]
    keys = ["rps", "p50_ms", "p95_ms", "p99_ms", "avg_ms"]
    totals: Dict[str, float] = {k: 0.0 for k in keys}
    for s in stats_list:
        for k in keys:
            try:
                totals[k] += float(s.get(k, 0) or 0)
            except ValueError:
                pass
    n = len(stats_list)
    averaged = dict(stats_list[-1])  # copy for non-averaged fields
    for k in keys:
        averaged[k] = f"{totals[k] / n:.2f}"
    # Sum requests/failures across runs
    averaged["requests"] = str(sum(int(float(s.get("requests", 0) or 0)) for s in stats_list))
    averaged["failures"] = str(sum(int(float(s.get("failures", 0) or 0)) for s in stats_list))
    return averaged


def write_summary(run_dir: Path, results: List[Dict]) -> None:
    summary_path = run_dir / "summary.md"
    json_path = run_dir / "summary.json"

    grouped: Dict[str, List[Dict]] = {}
    for item in results:
        grouped.setdefault(item["scenario"], []).append(item)

    lines = [
        "# Gateway Benchmark Summary",
        "",
        f"Generated at: {dt.datetime.now(dt.timezone.utc).isoformat()}",
        "",
    ]

    for scenario_name, scenario_results in grouped.items():
        runs = scenario_results[0].get("runs", 1)
        runs_note = f" _(averaged over {runs} runs)_" if runs > 1 else ""
        lines.append(f"## Scenario: {scenario_name}{runs_note}")
        lines.append("")
        lines.append("| Gateway | Requests | Failure % | RPS | p50 (ms) | p95 (ms) | p99 (ms) | Avg (ms) |")
        lines.append("| :------ | -------: | --------: | --: | -------: | -------: | -------: | -------: |")

        for item in sorted(scenario_results, key=lambda x: x["gateway"]):
            s = item["stats"]
            lines.append(
                "| {gateway} | {requests} | {failure_pct}% | {rps} | {p50_ms} | {p95_ms} | {p99_ms} | {avg_ms} |".format(
                    gateway=item["gateway"],
                    requests=s["requests"],
                    failure_pct=failure_pct(s["requests"], s["failures"]),
                    rps=s["rps"],
                    p50_ms=s["p50_ms"],
                    p95_ms=s["p95_ms"],
                    p99_ms=s["p99_ms"],
                    avg_ms=s["avg_ms"],
                )
            )
        lines.append("")

    summary_path.write_text("\n".join(lines), encoding="utf-8")
    json_path.write_text(json.dumps(results, indent=2), encoding="utf-8")

    print(f"\nWrote: {summary_path}")
    print(f"Wrote: {json_path}")


def main() -> int:
    args = parse_args()

    project_root = Path(__file__).resolve().parent.parent
    os.chdir(project_root)

    load_dotenv(project_root / args.dotenv)
    cfg = load_config(project_root / args.config)

    selected_gateways = parse_selection(args.gateways)
    selected_scenarios = parse_selection(args.scenarios)

    gateways = pick_gateways(cfg, selected_gateways)
    scenarios = pick_scenarios(cfg, selected_scenarios)

    timestamp = dt.datetime.now().strftime("%Y%m%d-%H%M%S")
    run_dir = project_root / args.out_dir / timestamp
    run_dir.mkdir(parents=True, exist_ok=True)

    results: List[Dict] = []
    for scenario in scenarios:
        for gateway_name, gateway_cfg in gateways.items():
            repeat_stats: List[Dict[str, str]] = []
            for run_idx in range(args.repeat):
                stats_file, _prefix = run_locust(
                    gateway_name, gateway_cfg, scenario, run_dir, args.dry_run, run_idx
                )
                if args.dry_run:
                    continue
                stats = read_locust_stats(stats_file)
                repeat_stats.append(stats)

            if args.dry_run:
                continue

            final_stats = average_stats(repeat_stats)
            results.append(
                {
                    "gateway": gateway_name,
                    "scenario": str(scenario.get("name")),
                    "runs": args.repeat,
                    "stats": final_stats,
                    "stats_files": [
                        str(run_dir / f"{gateway_name}-{scenario.get('name')}{'-run' + str(i + 1) if i > 0 else ''}_stats.csv")
                        for i in range(args.repeat)
                    ],
                }
            )

    if not args.dry_run:
        write_summary(run_dir, results)
    else:
        print("\nDry run complete. No load test was executed.")

    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except subprocess.CalledProcessError as exc:
        print(f"Benchmark command failed with exit code {exc.returncode}", file=sys.stderr)
        raise SystemExit(exc.returncode)
    except Exception as exc:  # pylint: disable=broad-except
        print(f"Error: {exc}", file=sys.stderr)
        raise SystemExit(1)
