# Autoscaling — dev branch

This branch exists only to track what's currently deployed to these regions:

* dev-us-east-2-beta
* dev-eu-central-1-alpha

We don't *quite* use the release yaml files directly, because there are some config differences that
we want to preserve.

Currently these are:

```js
// Agent:
config.billing = {
      "url": "http://neon-internal-api.aws.neon.build/billing/api/v1",
      "cpuMetricName": "effective_compute_seconds",
      "activeTimeMetricName": "active_time_seconds",
      "collectEverySeconds": 4,
      "pushEverySeconds": 24,
      "pushTimeoutSeconds": 2
}
```

### Other regions

The other "dev" regions have the following deployed:

* dev-eu-west-1-zeta: `autoscale-scheduler-disabled.yaml`
