---
id: TEST-T-0001
type: task
title: References to another project
status: todo
author: jose
parent: OTHER-US-0031
created: 2026-01-05T09:00:00Z
updated: 2026-01-05T09:00:00Z
links:
  - { kind: relates_to, target: OTHER-US-0031 }
  - { kind: relates_to, target: OTHER/TEST-US-0001 }
---

## Description

`parent` is always local (E-ID-KEY); a link to another project must carry its
project qualifier, and a qualifier that disagrees with the id is an error too.
