declare module "k6/experimental/fs" {
  export interface File {
    read(buffer: Uint8Array): Promise<number>;
  }

  export function open(path: string): Promise<File>;
}
