interface ConfigField {
  type: "text" | "checkbox";
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
}

interface CredentialField extends ConfigField {
  sensitive?: boolean;
}

interface DestinationTypeReference {
  type: string;
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
}

export type {
  Destination,
  Filter,
  ConfigField,
  CredentialField,
  DestinationTypeReference,
};
