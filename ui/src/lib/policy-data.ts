export type PolicyType = 'post_plan' | 'pre_plan' | 'pre_apply' | 'trigger' | 'login';

// ─── Templates ────────────────────────────────────────────────────────────────

export interface PolicyTemplate {
	name: string;
	description: string;
	body: string;
}

const BLANK_PLAN_POLICY = `package crucible

plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

deny_msgs[msg] {
  # Add denial rules here — each matching rule blocks the run
  false
  msg := ""
}

warn_msgs[msg] {
  # Add warning rules here — non-blocking, shown to the operator
  false
  msg := ""
}
`;

export const policyTemplates: Record<PolicyType, PolicyTemplate[]> = {
	post_plan: [
		{
			name: 'Block destroys',
			description: 'Prevent any resource from being deleted via an automated run.',
			body: `package crucible

plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

deny_msgs[msg] {
  input.resource_changes[_].change.actions[_] == "delete"
  msg := "destroy operations are not permitted via automated runs — use an explicit destroy run"
}

warn_msgs[msg] {
  input.resource_changes[_].change.actions[_] == "update"
  msg := sprintf("resource %v will be modified", [input.resource_changes[_].address])
}
`
		},
		{
			name: 'Enforce instance type allowlist',
			description: 'Deny any EC2 instance not in the approved size list.',
			body: `package crucible

allowed_instance_types := {"t3.micro", "t3.small", "t3.medium", "t3.large"}

plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    [],
    "trigger": [],
  }
}

deny_msgs[msg] {
  r := input.resource_changes[_]
  r.type == "aws_instance"
  r.change.actions[_] != "delete"
  instance_type := r.change.after.instance_type
  not allowed_instance_types[instance_type]
  msg := sprintf("instance type %v is not in the approved list: %v", [instance_type, allowed_instance_types])
}
`
		},
		{
			name: 'Require resource tags',
			description: 'Ensure all resources carry the required cost/ownership tags.',
			body: `package crucible

required_tags := {"owner", "environment", "cost-centre"}

plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    [],
    "trigger": [],
  }
}

deny_msgs[msg] {
  r := input.resource_changes[_]
  r.change.actions[_] != "delete"
  r.change.actions[_] != "no-op"
  tags := object.get(r.change.after, "tags", {})
  missing := required_tags - {k | tags[k]}
  count(missing) > 0
  msg := sprintf("resource %v is missing required tags: %v", [r.address, missing])
}
`
		},
		{
			name: 'Limit blast radius',
			description: 'Block plans that change more than N resources at once.',
			body: `package crucible

max_changes := 10

plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

changing := [r | r := input.resource_changes[_]; r.change.actions[_] != "no-op"]

deny_msgs[msg] {
  count(changing) > max_changes
  msg := sprintf("this plan modifies %v resources — limit is %v; split into smaller changes",
    [count(changing), max_changes])
}

warn_msgs[msg] {
  count(changing) > (max_changes / 2)
  count(changing) <= max_changes
  msg := sprintf("this plan modifies %v resources — approaching the limit of %v",
    [count(changing), max_changes])
}
`
		},
		{
			name: 'Blank',
			description: 'Start from an empty template.',
			body: BLANK_PLAN_POLICY
		}
	],

	pre_plan: [
		{
			name: 'Block plan on missing variable',
			description: 'Deny plans where a required input variable is not set.',
			body: `package crucible

required_vars := {"environment", "region"}

plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    [],
    "trigger": [],
  }
}

deny_msgs[msg] {
  v := required_vars[_]
  not input.variables[v]
  msg := sprintf("required variable '%v' is not set", [v])
}
`
		},
		{
			name: 'Blank',
			description: 'Start from an empty template.',
			body: BLANK_PLAN_POLICY
		}
	],

	pre_apply: [
		{
			name: 'Final safety check — no deletes',
			description: 'Last-chance block before apply: deny if any resource would be deleted.',
			body: `package crucible

plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

deny_msgs[msg] {
  r := input.resource_changes[_]
  r.change.actions[_] == "delete"
  msg := sprintf("apply blocked: resource %v would be destroyed — confirm with an explicit destroy run",
    [r.address])
}

warn_msgs[msg] {
  r := input.resource_changes[_]
  r.change.actions[_] == "update"
  msg := sprintf("resource %v will be modified in-place", [r.address])
}
`
		},
		{
			name: 'Blank',
			description: 'Start from an empty template.',
			body: BLANK_PLAN_POLICY
		}
	],

	trigger: [
		{
			name: 'Trigger downstream stack',
			description: 'Automatically trigger a downstream stack when this one completes with changes.',
			body: `package crucible

# Replace with the downstream stack's UUID
downstream_stack_id := "<stack-uuid>"

trigger := result {
  result := {
    "deny":    [],
    "warn":    [],
    "trigger": stack_ids,
  }
}

# Only trigger if something actually changed (not a no-op plan)
stack_ids := [downstream_stack_id] {
  changed := [r | r := input.resource_changes[_]; r.change.actions[_] != "no-op"]
  count(changed) > 0
}

stack_ids := [] { true }
`
		},
		{
			name: 'Blank',
			description: 'Start from an empty template.',
			body: `package crucible

trigger := result {
  result := {
    "deny":    [],
    "warn":    [],
    "trigger": stack_ids,
  }
}

stack_ids := [] { true }
`
		}
	],

	login: [
		{
			name: 'Restrict by IdP group',
			description: 'Allow login only for users belonging to a specific IdP group.',
			body: `package crucible

# Members of this group are allowed to log in
allowed_group := "platform-admins"

login := result {
  result := {
    "deny": deny_msgs,
    "warn": [],
  }
}

deny_msgs[msg] {
  not input.user.groups[_] == allowed_group
  msg := sprintf("login denied: user %v is not a member of the '%v' group",
    [input.user.email, allowed_group])
}
`
		},
		{
			name: 'Require any group membership',
			description: 'Deny login for users with no IdP groups (e.g. service accounts).',
			body: `package crucible

login := result {
  result := {
    "deny": deny_msgs,
    "warn": [],
  }
}

deny_msgs[msg] {
  count(input.user.groups) == 0
  msg := sprintf("login denied: user %v has no group memberships", [input.user.email])
}
`
		},
		{
			name: 'Blank',
			description: 'Start from an empty template.',
			body: `package crucible

login := result {
  result := {
    "deny": deny_msgs,
    "warn": [],
  }
}

deny_msgs[msg] {
  # Add denial rules here
  false
  msg := ""
}
`
		}
	]
};

// ─── Sample inputs for the dry-run sandbox ────────────────────────────────────

export const sampleInputs: Record<PolicyType, string> = {
	post_plan: JSON.stringify(
		{
			format_version: '1.2',
			resource_changes: [
				{
					address: 'aws_instance.web',
					type: 'aws_instance',
					name: 'web',
					change: {
						actions: ['create'],
						before: null,
						after: { instance_type: 't3.micro', tags: { owner: 'platform', environment: 'prod' } }
					}
				},
				{
					address: 'aws_s3_bucket.data',
					type: 'aws_s3_bucket',
					name: 'data',
					change: {
						actions: ['delete'],
						before: { bucket: 'my-data-bucket' },
						after: null
					}
				}
			],
			variables: {
				environment: { value: 'prod' },
				region: { value: 'eu-west-1' }
			}
		},
		null,
		2
	),

	pre_plan: JSON.stringify(
		{
			format_version: '1.2',
			resource_changes: [],
			variables: {
				environment: { value: 'prod' },
				region: { value: 'eu-west-1' }
			}
		},
		null,
		2
	),

	pre_apply: JSON.stringify(
		{
			format_version: '1.2',
			resource_changes: [
				{
					address: 'aws_s3_bucket.data',
					type: 'aws_s3_bucket',
					name: 'data',
					change: {
						actions: ['delete'],
						before: { bucket: 'my-data-bucket' },
						after: null
					}
				}
			],
			variables: {}
		},
		null,
		2
	),

	trigger: JSON.stringify(
		{
			format_version: '1.2',
			resource_changes: [
				{
					address: 'aws_instance.web',
					type: 'aws_instance',
					name: 'web',
					change: { actions: ['update'], before: { instance_type: 't3.small' }, after: { instance_type: 't3.medium' } }
				}
			],
			variables: {}
		},
		null,
		2
	),

	login: JSON.stringify(
		{
			user: {
				sub: 'auth0|abc123',
				email: 'alice@example.com',
				groups: ['platform-admins', 'developers']
			}
		},
		null,
		2
	)
};

// ─── Input schema explorer ─────────────────────────────────────────────────────

export interface SchemaField {
	path: string;
	type: string;
	description: string;
}

export interface InputSchema {
	summary: string;
	sample: string;
	fields: SchemaField[];
}

export const inputSchemas: Record<PolicyType, InputSchema> = {
	post_plan: {
		summary: 'Receives the full Terraform/OpenTofu plan JSON after a plan completes.',
		sample: `input.resource_changes[_] = {
  "address": "aws_instance.web",
  "type":    "aws_instance",
  "name":    "web",
  "change": {
    "actions": ["create"],   // "create"|"update"|"delete"|"no-op"
    "before":  null,         // state before (null for create)
    "after":   { "instance_type": "t3.micro", ... }
  }
}`,
		fields: [
			{
				path: 'input.resource_changes',
				type: 'array',
				description: 'All resources being created, updated, deleted, or left unchanged.'
			},
			{
				path: 'input.resource_changes[_].address',
				type: 'string',
				description: 'Full resource address, e.g. "aws_instance.web".'
			},
			{
				path: 'input.resource_changes[_].type',
				type: 'string',
				description: 'Resource type, e.g. "aws_instance".'
			},
			{
				path: 'input.resource_changes[_].change.actions',
				type: 'array<string>',
				description: 'One or more of: "create", "update", "delete", "no-op".'
			},
			{
				path: 'input.resource_changes[_].change.before',
				type: 'object | null',
				description: 'Resource attributes before the change (null for creates).'
			},
			{
				path: 'input.resource_changes[_].change.after',
				type: 'object | null',
				description: 'Resource attributes after the change (null for deletes).'
			},
			{
				path: 'input.variables',
				type: 'object',
				description: 'Input variables as { "name": { "value": "..." } }.'
			},
			{
				path: 'input.configuration',
				type: 'object',
				description: 'Root module configuration (providers, resources, outputs).'
			}
		]
	},

	pre_plan: {
		summary:
			'Receives the same plan JSON shape as post_plan — useful for validating variable values before the plan runs.',
		sample: `input.variables = {
  "environment": { "value": "production" },
  "region":      { "value": "eu-west-1" }
}`,
		fields: [
			{
				path: 'input.variables',
				type: 'object',
				description: 'Input variables as { "name": { "value": "..." } }.'
			},
			{
				path: 'input.resource_changes',
				type: 'array',
				description: 'Resource changes (same shape as post_plan).'
			},
			{
				path: 'input.configuration',
				type: 'object',
				description: 'Root module configuration.'
			}
		]
	},

	pre_apply: {
		summary: 'Same plan JSON as post_plan, evaluated just before apply executes.',
		sample: `input.resource_changes[_] = {
  "address": "aws_s3_bucket.data",
  "change": {
    "actions": ["delete"],
    "before":  { "bucket": "my-data-bucket", ... },
    "after":   null
  }
}`,
		fields: [
			{
				path: 'input.resource_changes',
				type: 'array',
				description: 'Resources being changed — same shape as post_plan.'
			},
			{
				path: 'input.resource_changes[_].change.actions',
				type: 'array<string>',
				description: '"create" | "update" | "delete" | "no-op".'
			},
			{
				path: 'input.variables',
				type: 'object',
				description: 'Input variables.'
			}
		]
	},

	trigger: {
		summary:
			'Receives the completed plan JSON. Return stack UUIDs in the "trigger" array to queue downstream runs.',
		sample: `input.resource_changes[_].change.actions  // same as post_plan

// Return downstream stack UUIDs:
trigger := { "deny": [], "warn": [], "trigger": ["<stack-uuid>"] }`,
		fields: [
			{
				path: 'input.resource_changes',
				type: 'array',
				description: 'All resource changes from the completed run.'
			},
			{
				path: 'input.resource_changes[_].change.actions',
				type: 'array<string>',
				description: '"create" | "update" | "delete" | "no-op".'
			},
			{
				path: 'output trigger[_]',
				type: 'string (UUID)',
				description: 'Stack IDs to trigger after this run. Return [] to trigger nothing.'
			}
		]
	},

	login: {
		summary: "Receives the authenticating user's identity from the IdP.",
		sample: `input.user = {
  "sub":    "auth0|abc123",
  "email":  "alice@example.com",
  "groups": ["platform-admins", "developers"]
}`,
		fields: [
			{
				path: 'input.user.sub',
				type: 'string',
				description: 'The subject claim from the IdP token (unique user identifier).'
			},
			{
				path: 'input.user.email',
				type: 'string',
				description: "User's email address."
			},
			{
				path: 'input.user.groups',
				type: 'array<string>',
				description: 'Group memberships from the IdP (e.g. from the "groups" claim).'
			}
		]
	}
};
