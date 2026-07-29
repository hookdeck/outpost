# Outpost Loadtesting Documentation

This directory contains documentation for the Outpost loadtesting framework.

## Overview

The Outpost loadtesting framework provides tools and scripts to simulate realistic load conditions and evaluate the performance and reliability of the Outpost service under various workloads and scenarios. These tests are designed to measure throughput, verify event delivery, and monitor system resource usage to determine capacity limits and identify bottlenecks.

## High-Level Workflow

1. **Provision Infrastructure**: Set up the desired infrastructure environment you want to test, specifying resource limits.

2. **Setup Monitoring**: Deploy Prometheus and Grafana to collect performance metrics during testing.

3. **Configure Test Scenario**: Adjust the loadtest configuration files to simulate your desired testing conditions.

4. **Run Tests**: Execute the throughput and verification test scripts to generate load and verify delivery.

5. **Analyze Results**: Evaluate the Outpost service capacity using two main data sources:
   - **k6 Metrics**: External perspective showing how the service behaves from a client's point of view.
   - **Grafana Dashboards**: Internal perspective showing resource consumption and system behavior under load.

## Available Tools and Scripts

- **Local Kubernetes Infrastructure**: A configurable Kubernetes setup with resource limits that allows for realistic testing environments.

- **Prometheus & Grafana**: Monitoring tools to observe resource utilization during tests, helping identify bottlenecks and performance issues.

- **Mock Webhook Destination**: A simulated event sink used to verify event delivery and measure end-to-end latency.

- **k6 Testing Scripts**: Two main testing flows:
  - **Events Throughput**: Publishes events to the Outpost service to test throughput capacity.
  - **Events Verify**: Checks the mock webhook destination to verify successful delivery of events.
  
  These scripts are coordinated using Redis to maintain test state and correlate published events with verifications.

## Documentation Contents

- [Infrastructure Setup](./infra.md): Instructions for setting up the testing infrastructure, including local Kubernetes deployments and monitoring tools.

- [Loadtest Workflow](./loadtest.md): Detailed guide on configuring and running loadtests, verifying results, and interpreting the data.

## Getting Started

1. Set up your testing infrastructure by following the [infrastructure guide](./infra.md).
2. Configure and run your loadtests using the steps in the [loadtest workflow](./loadtest.md).

For questions or issues, please contact the Outpost team.
