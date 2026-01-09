## Resource controls and monitoring

- Use Docker resource limits per container where appropriate.
- Monitor usage with `docker stats` or your host monitoring stack.
- Best practices:
  - Keep per-app DBs lean; drop unused databases.
  - Review `silexa report status` output alongside system metrics.
  - Increase limits only when justified by workload; document changes in manager feedback.
