#!/bin/bash
echo "Hello World from {{.Name}} on branch {{.Branch}} (slot {{.Slot}})"
echo "Web port: {{.Port.web}}"
echo "API port: {{.Port.api}}"
