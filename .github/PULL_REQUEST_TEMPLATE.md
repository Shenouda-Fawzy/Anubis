name: Pull request

description: A change to Anubis

body:
  - type: markdown
    attributes:
      value: |
        Thanks for contributing. Please read CONTRIBUTING.md — in particular the
        section on changing the review pipeline.

  - type: textarea
    id: user-visible
    attributes:
      label: What changes for a user?
      description: |
        Not what changed in the code — what a user of the action or CLI will
        experience differently. If you touched a prompt, say what behavior you
        expect to change.
    validations:
      required: true

  - type: textarea
    id: testing
    attributes:
      label: How was this tested?
      description: |
        New tests, plus which make target you ran. If you verified against a
        real provider, say which, and paste the review output with the diff
        removed.
    validations:
      required: true

  - type: textarea
    id: docs
    attributes:
      label: Documentation
      description: |
        If you changed flags, inputs, outputs, environment variables or
        behavior, did you update README.md, AGENTS.md and action.yml? These
        have drifted before and shipping them out of sync is what breaks
        consumers.
    validations:
      required: false

  - type: dropdown
    id: area
    attributes:
      label: Which area does this touch?
      options:
        - Review pipeline (agents, coordinator, prompts)
        - GitHub Action packaging (action.yml, Dockerfile, outputs)
        - CLI
        - Tests
        - Docs only
    validations:
      required: true

  - type: checkboxes
    id: checklist
    attributes:
      label: Checklist
      options:
        - label: make fmt-check, make vet and make test all pass.
        - label: I ran go test -race if I touched concurrent code.
        - label: I added or updated a test for the new behavior.
        - label: "docker build . works, if I touched the image."
        - label: I did not add a dependency, or I explained why in the description.
        - label: I did not log diff, prompt or finding content.
