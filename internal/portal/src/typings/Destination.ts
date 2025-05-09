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

interface Destination {
  id: string;
  type: string;
  config: Record<string, any>;
  topics: string[];
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
  ConfigField,
  CredentialField,
  DestinationTypeReference,
};
