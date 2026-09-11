package main

var reviewTask = `# Pull Request

<pr>
  <repository>%s</repository>
  <pull_request>%d</pull_request>
  <title>%s</title>

  <description>
   %s
  </description>
</pr>

# Diff

%s

# Specialist Findings

The following findings were independently produced by specialized review agents.

Treat them as **candidate findings**, not established facts.

<reviewer_findings>

  %s

</reviewer_findings>

# Task

Perform the final coordination pass.

For every candidate finding:

1. Determine whether it is actually valid.
2. Verify it against the available code and diff.
3. Reject speculative or unsupported findings.
4. Deduplicate overlapping findings.
5. Merge findings describing the same root cause.
6. Correct the category when necessary.
7. Correct the severity when necessary.
8. Keep only actionable findings that are relevant to this pull request.

Then return the final findings using the required output schema.
`
