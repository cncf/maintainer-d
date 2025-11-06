# Project Onboarding 

Project Sandbox Onboarding, often abbreviated to, "project onboarding" or just
"onboarding", comprises the set of tasks that must be carried out by both the
Project maintainer team and CNCF Staff

Onboarding activities are tracked in a formatted GitHub Issue in the
cncf/sandbox repo.

The CNCF keeps an internal record of Projects and the Maintainers who work on
them.

The internal record associates Project Maintainers with their project. The
record is stored internally out of public view, because we ask maintainers
to give us an email address and a lot of maintainerd do not want to share the 
email address that they use to register their participation in a CNCF Project,

1. A Maintainer's participation is registered with maintainerd when their
details are recorded on the internal record in association with their project.
This requires manual effort on the part of the CNCF Projects Team at the moment,
in future we would like to store the association of our maintainers with their
projects in a combination of The Project Control Center and OpenProfile.dev both
operated by LFX team in the Linux Foundation.

2. Once the internal records are present, maintainer-d can read those records and
store them in the maintainer-d database.

# FOSSA User Onboarding Workflow

## Label Command

Registered project maintainers can use the `/label` command to indicate their preference for license scanning tools. This command adds either the `fossa` or `snyk` label to the project's onboarding issue.

### Usage

```
/label fossa
```
or
```
/label snyk
```

### Authorization

Only registered maintainers in the maintainer-d database can execute this command. When an unauthorized user attempts to use the command, they will receive a notification that they are not authorized.

### Workflow

1. When a registered maintainer wants to indicate their preference for a license scanning tool, they can add a comment with the `/label` command followed by either `fossa` or `snyk`.

2. The maintainer-d server validates that:
   - The command format is correct
   - The label name is either `fossa` or `snyk`
   - The user is a registered maintainer for the project
   - The project exists in the maintainer-d database

3. If validation passes, the server adds the requested label to the issue.

4. A confirmation comment is posted indicating which tool was selected.

## FOSSA Onboarding Process

1. When a project chooses to use FOSSA for license scanning, a registered maintainer or CNCF Staff member can add the fossa label using the `/label fossa` command or by manually adding the label.

2. Labelling the issue triggers an event that is sent to the maintainer-d webhook.

3. Then maintainer-d proceeds to setup the project in the CNCF FOSSA service instance.

4.1 A FOSSA Team is created using the name project name in the issue

4.2 For each registered maintainer, an invitation to join CNCF FOSSA is sent to
the maintainer

4.3 The onboarding issue is updated with a comment that tags the maintainers
using their GitHub Handle and explains to them that they need to accept their invite.

4.4 Inviting a user to join CNCF FOSSA does not tie their invitation to the team
that we created in step 5.1, and this means that a separate step of, "adding the
invited user to their FOSSA Team" needs to be carried out.

Maintainers or CNCF Stff can add a "/fossa-invite accepted" comment to 
the issue when a maintainer has accepted their invitation to join CNCF FOSSA 

For every registered maintainer, maintainer-d will check to see if they 
are now a user on CNCF FOSSA and will add them to their team. 

The onboarding issue is updated with a comment that describes actions taken 
and the next steps that need to be taken by the maintainers (namely, they now need to 
import their project repositories int to FOSSA which checks the project's compliance with 
the CNCF's 3rd-Party license policy
