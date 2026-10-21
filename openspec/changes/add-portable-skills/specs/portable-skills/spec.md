# Portable Skills

## Purpose

Enable teachers to exchange self-contained Agent Skills Markdown documents with other agents, create a reusable English writing review skill, and bind imported skills to multiple class-scoped agents without changing existing permissions or executing uploaded code.

## ADDED Requirements

### Requirement: Parse and preview both formats
The system SHALL retain course-format support and accept YAML-frontmatter SKILL.md files with name, description and Markdown body. Teachers SHALL preview without persistence and configure local display name, keywords, subject, tools and enabled state. Invalid YAML, duplicate keys, aliases, malformed names and oversize inputs MUST produce readable 400 errors. Missing local keywords SHALL allow preview but prevent saving.

#### Scenario: External standalone import
- **WHEN** a teacher previews a standard file with no application metadata
- **THEN** its complete body is shown, it defaults to disabled, and the teacher can set local keywords before saving a new skill

#### Scenario: Existing format
- **WHEN** a teacher saves a valid Chinese-header skill
- **THEN** its existing routing and binding behavior remains available within the new documented size limits

### Requirement: Portable export and round trip
Teachers SHALL export a same-class skill as SKILL.md with valid portable name and description. Body, local settings and existing standard optional metadata SHALL survive export and reimport. Export SHALL omit database identifiers, credentials and class-specific knowledge content. Foreign or absent ids MUST have identical 404 responses, students MUST receive 403 and unauthenticated users MUST receive 401 for preview/export.

#### Scenario: Round trip
- **WHEN** a teacher exports a course-format skill and imports it again
- **THEN** body, display name, keywords, subject, enabled state and tool references are preserved, with MCP references revalidated at save

### Requirement: Honest dependency and permission boundaries
The system MUST NOT execute scripts or install dependencies from imported text, and MUST NOT grant permissions from external allowed-tools. The preview and export UI SHALL disclose the standalone-file scope and warn about references/scripts/assets or external tool declarations that need adaptation.

#### Scenario: External coding skill
- **WHEN** imported text declares shell tools and references scripts
- **THEN** the declarations remain inspectable, warnings explain unsupported dependencies, and no shell or new MCP permission is granted

### Requirement: Reuse and English writing skill
The repository SHALL include a valid standalone english-writing-review/SKILL.md, and a teacher SHALL be able to bind a saved skill to multiple same-class agents. The English skill SHALL distinguish required corrections from optional style suggestions and preserve the writer's meaning.

#### Scenario: Multiple agents
- **WHEN** two same-class agents bind the imported English skill and receive matching writing questions
- **THEN** both select that skill and include its instructions in model context; cross-class binding remains denied
