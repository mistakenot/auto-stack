"""CLI shell over the Harness class — `uv run harness <cmd>`."""

from __future__ import annotations

import json
import sys

import click

from harness.core import Harness


@click.group()
@click.pass_context
def main(ctx: click.Context) -> None:
    """auto-skill end-to-end test harness.

    Manages a Docker Compose stack (git-server + system-under-test) and
    provides commands to run auto-skill against a real remote git repo.
    """
    ctx.ensure_object(dict)
    ctx.obj["harness"] = Harness()


@main.command()
@click.option("--no-build", is_flag=True, help="Skip Docker image rebuild.")
@click.pass_context
def up(ctx: click.Context, no_build: bool) -> None:
    """Start the harness stack (git-server + SUT).

    Builds images from source, starts containers, and blocks until
    all services report healthy. The SUT container compiles the latest
    auto binary from the monorepo.
    """
    h: Harness = ctx.obj["harness"]
    click.echo("Starting harness stack...")
    h.up(build=not no_build)
    click.echo("Harness is up and healthy.")


@main.command()
@click.pass_context
def down(ctx: click.Context) -> None:
    """Tear down the harness stack and remove volumes."""
    h: Harness = ctx.obj["harness"]
    h.down()
    click.echo("Harness is down.")


@main.command()
@click.pass_context
def status(ctx: click.Context) -> None:
    """Show health status of all harness services."""
    h: Harness = ctx.obj["harness"]
    s = h.status()
    click.echo(json.dumps(s, indent=2))


@main.command("run")
@click.argument("container")
@click.argument("cmd")
@click.pass_context
def run_cmd(ctx: click.Context, container: str, cmd: str) -> None:
    """Run a shell command inside a container.

    CONTAINER is the service name (sut or git-server).
    CMD is the shell command string to execute.
    """
    h: Harness = ctx.obj["harness"]
    r = h.run(container, cmd)
    if r.stdout:
        click.echo(r.stdout, nl=False)
    if r.stderr:
        click.echo(r.stderr, nl=False, err=True)
    sys.exit(r.exit_code)


@main.command()
@click.argument("subcmd")
@click.argument("args", nargs=-1)
@click.pass_context
def skill(ctx: click.Context, subcmd: str, args: tuple[str, ...]) -> None:
    """Run `auto skill <subcmd> [args...]` inside the SUT container.

    Examples:
      harness skill init -- --project -y
      harness skill add git@git-server:/repos/skills.git -- --skill deploy-checklist
      harness skill sync
    """
    h: Harness = ctx.obj["harness"]
    r = h.run_skill(subcmd, *args)
    if r.stdout:
        click.echo(r.stdout, nl=False)
    if r.stderr:
        click.echo(r.stderr, nl=False, err=True)
    sys.exit(r.exit_code)


if __name__ == "__main__":
    main()
