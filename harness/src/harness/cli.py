"""CLI shell over the scenario harness — `uv run harness <scenario> <cmd>`.

The first argument selects a scenario; the remaining commands drive its Compose
stack. Example:

    uv run harness skill-remote up
    uv run harness skill-remote run sut "auto skill sync --text"
    uv run harness skill-remote down
"""

from __future__ import annotations

import json
import sys

import click

from harness.scenarios.base import Scenario
from harness.scenarios.event_flow import EventFlowScenario
from harness.scenarios.skill_remote import SkillRemoteScenario

# Registry of available scenarios. Adding a scenario is one line here plus its
# module + compose dir — nothing else in the CLI changes.
SCENARIOS: dict[str, type[Scenario]] = {
    "skill-remote": SkillRemoteScenario,
    "event-flow": EventFlowScenario,
}


@click.group()
@click.argument("scenario", type=click.Choice(sorted(SCENARIOS)))
@click.pass_context
def main(ctx: click.Context, scenario: str) -> None:
    """auto-stack end-to-end test harness.

    SCENARIO selects a self-contained Compose stack (one of the choices above).
    Each scenario builds real images from monorepo source and drives the `auto`
    binary inside its containers.
    """
    ctx.ensure_object(dict)
    ctx.obj["scenario"] = SCENARIOS[scenario]()


@main.command()
@click.option("--no-build", is_flag=True, help="Skip Docker image rebuild.")
@click.pass_context
def up(ctx: click.Context, no_build: bool) -> None:
    """Start the scenario stack and run its readiness gates."""
    s: Scenario = ctx.obj["scenario"]
    click.echo(f"Starting scenario {s.name!r}...")
    s.up(build=not no_build)
    click.echo(f"Scenario {s.name!r} is up and ready.")


@main.command()
@click.option(
    "--keep-images",
    is_flag=True,
    help="Keep the images this stack built, so the next up reuses them. Same effect as HARNESS_KEEP_IMAGES=1.",
)
@click.pass_context
def down(ctx: click.Context, keep_images: bool) -> None:
    """Tear down the scenario stack, removing volumes and the images it built."""
    s: Scenario = ctx.obj["scenario"]
    s.down(remove_images=False if keep_images else None)
    kept = " (images kept)" if keep_images else ""
    click.echo(f"Scenario {s.name!r} is down{kept}.")


@main.command()
@click.pass_context
def status(ctx: click.Context) -> None:
    """Show health status of all scenario services."""
    s: Scenario = ctx.obj["scenario"]
    click.echo(json.dumps(s.status(), indent=2))


@main.command("run")
@click.argument("service")
@click.argument("cmd")
@click.pass_context
def run_cmd(ctx: click.Context, service: str, cmd: str) -> None:
    """Run a shell command inside a scenario SERVICE.

    SERVICE is the Compose service name (e.g. `sut`, `agent-1`, `auto-ui`).
    CMD is the shell command string to execute.
    """
    s: Scenario = ctx.obj["scenario"]
    r = s.run(service, cmd)
    if r.stdout:
        click.echo(r.stdout, nl=False)
    if r.stderr:
        click.echo(r.stderr, nl=False, err=True)
    sys.exit(r.exit_code)


if __name__ == "__main__":
    main()
