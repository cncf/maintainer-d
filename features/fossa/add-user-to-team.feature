@fossa @issue-comment
Feature: Add accepted FOSSA invitee to the project team
  When a maintainer (or a user assigned to the onboarding issue) confirms
  acceptance of an invitation to join the CNCF FOSSA organization
  via an onboarding issue comment, the server adds the maintainers
  (all registered for the project) to the project's FOSSA team and records the action.

  Background:
    Given a CNCF project "<project>" exists in maintainerd
    And an onboarding issue exists titled "[PROJECT ONBOARDING] <project>"
    And the project has a FOSSA team named "<project>"
    And maintainerd contains maintainer "<gh_user>" with email "<email>" for "<project>"
    # Authorization via assignees; no separate staff registry is required here

  Rule: Authorization
    - Maintainers of the project may trigger processing for their project.
    - Any GitHub user assigned to the onboarding issue may trigger processing for that project.

  Rule: Acceptance verification
    - The server verifies there is no active/pending invitation for the email before adding to the team.

  Rule: Command matching
    - The trigger must be exactly "/fossa-invite accepted" (case-sensitive, no extra whitespace).

  Rule: Privacy in public comments
    - Public comments must never include email addresses; reference maintainers by GitHub handle only.

  Rule: Scope of action
    - The command processes all registered maintainers for the project, not only the comment author.

  Rule: Unauthorized attempts
    - If the comment author is neither a registered maintainer of the project nor assigned to the onboarding issue,
      the server posts a denial comment, makes no FOSSA changes, and records an audit entry "FOSSA_ADD_MEMBER_DENIED".

  Scenario: Maintainer confirms acceptance via issue comment (all maintainers processed)
    Given a valid GitHub webhook signature
    And an issue comment with body "/fossa-invite accepted" authored by "<gh_user>" on the onboarding issue
    When the onboarding server processes the comment
    Then the server verifies acceptance status for each registered maintainer of "<project>"
    And for each maintainer without a pending invitation who is not yet a member, the server adds them as "Team Admin"
    And for maintainers already in the team, the server takes no action
    And the server posts a summary comment listing outcomes per maintainer by GitHub handle only
    And audit log entries record action "FOSSA_ADD_MEMBER" for maintainers newly added

  Scenario: Some maintainers are already members
    Given at least one registered maintainer is already a member of the FOSSA team
    When a maintainer comments "/fossa-invite accepted" on the onboarding issue
    Then the server performs membership checks for all registered maintainers
    And the server posts a comment listing GitHub handles already in the team
    

  Scenario: Some maintainers still have pending invitations
    Given one or more registered maintainers have a pending FOSSA invitation
    When a maintainer comments "/fossa-invite accepted" on the onboarding issue
    Then the server skips adding those maintainers with pending invitations
    And the server posts a comment indicating which GitHub handles still have pending invitations
    

  Scenario: Issue assignee triggers addition for all project maintainers
    Given the issue comment author is assigned to the onboarding issue
    And the maintainer "<gh_user>" exists in maintainerd for the project with email "<email>"
    When the staff member comments "/fossa-invite accepted" on the onboarding issue
    Then the server processes all registered maintainers as per the main scenario
    And the server posts a summary comment listing actions per maintainer by handle
    And audit log entries record action "FOSSA_ADD_MEMBER" for added maintainers

  Scenario: Unauthorized actor is rejected
    Given the issue comment author is neither a maintainer of the project nor assigned to the onboarding issue
    When they comment "/fossa-invite accepted" on the onboarding issue
    Then the server posts a comment including "You are not authorized to perform this action"
    And no FOSSA changes are made
    And an audit log entry records action "FOSSA_ADD_MEMBER_DENIED"

  Scenario: Team missing is created and members added
    Given no FOSSA team exists for the project
    And the maintainer comments "/fossa-invite accepted" on the onboarding issue
    When the onboarding server processes the comment
    Then the server creates a FOSSA team named "<project>"
    And the server processes all registered maintainers:
      - add any not-yet-members as "Team Admin"
      - leave existing members unchanged
    And the server posts a comment including "Created FOSSA team <project>" and lists handle outcomes
    And audit log entries record "FOSSA_TEAM_CREATED" and "FOSSA_ADD_MEMBER" for added maintainers

  Scenario: Non-matching comment is ignored
    Given a comment body that does not exactly match "/fossa-invite accepted"
    When the onboarding server processes the comment
    Then the server takes no action on FOSSA membership
    And no audit log entry is created

  Scenario: Comment author is not a registered maintainer
    Given the issue comment author is not a registered maintainer for the project
    When the author comments "/fossa-invite accepted" on the onboarding issue
    Then the server posts a comment refusing the action due to missing registration
    And no FOSSA changes are made
    And an audit log entry records action "FOSSA_ADD_MEMBER_DENIED"

  
