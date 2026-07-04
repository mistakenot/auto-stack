"""Scenario packages for the auto-stack harness.

Each scenario pairs a self-contained Compose stack under
`harness/scenarios/<name>/` with a Python module here that subclasses
`Scenario` (compose path, service list, readiness gates, and a thin command
DSL). Adding a scenario is purely additive: a new folder plus a new module,
touching no existing scenario.
"""
