interface ConfigField {
  type: "text" | "checkbox" | "key_value_map" | "select";
  label: string;
  description: string;
  key: string;
  required: boolean;
  default?: string;
  disabled?: boolean;
  min?: number;
  max?: number;
  step?: number;
  minlength?: number;
  maxlength?: number;
  pattern?: string;
  options?: Array<{ label: string; value: string }>;
  key_placeholder?: string;
  value_placeholder?: string;
}

interface CredentialField extends ConfigField {
  sensitive?: boolean;
}

interface DestinationTypeReference {
  type: string;
  // "form" (also when absent, as in API v1) means the portal's create form.
  // Any other value, today "external" (mcp), means the type is created outside
  // the portal and `instructions` replace the form.
  create_mode?: string;
  config_fields: ConfigField[];
  credential_fields: CredentialField[];
  instructions: string;
  label: string;
  description: string;
  setup_link?: {
    href: string;
    cta: string;
  };
  icon: string;
}

// Filter type for event matching using JSON schema syntax
// Supports operators: $eq, $neq, $gt, $gte, $lt, $lte, $in, $nin, $startsWith, $endsWith, $exist, $or, $and, $not
type Filter = Record<string, any> | null;

interface Destination {
  id: string;
  type: string;
  config: Record<string, any>;
  topics: string[];
  filter?: Filter;
  credentials: Record<string, any>;
  label: string;
  description: string;
  target: string;
  target_url?: string;
  disabled_at: string;
  created_at: string;
  // Set on destinations that expire, such as MCP subscriptions. Absent or null
  // means no expiry.
  expires_at?: string | null;
  metadata?: Record<string, string>;
}

// The part of a v2 GET /topics entry the portal keeps: a deprecated topic and
// its replacement, if any.
interface TopicDeprecation {
  name: string;
  replaced_by?: string;
}

export type {
  Destination,
  Filter,
  ConfigField,
  CredentialField,
  DestinationTypeReference,
  TopicDeprecation,
};
