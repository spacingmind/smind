// varint.ts — the protobuf wire-format primitives proto.ts builds on:
// varint encode/decode and a byte-array writer. Kept separate from proto.ts
// so the wire-format plumbing is easy to read independent of which relay
// messages use it.

/** A growable byte buffer, since message sizes aren't known up front. */
export class ByteWriter {
  private chunks: number[] = [];

  writeByte(b: number): void {
    this.chunks.push(b & 0xff);
  }

  writeBytes(bytes: Uint8Array): void {
    for (const b of bytes) this.chunks.push(b);
  }

  /** Unsigned LEB128 varint, as protobuf's wire format uses throughout. */
  writeVarint(value: number): void {
    if (value < 0) throw new Error(`varint: negative value ${value}`);
    let v = value;
    while (v > 0x7f) {
      this.writeByte((v & 0x7f) | 0x80);
      v = Math.floor(v / 128); // >>> would truncate to 32 bits; values here can exceed that.
    }
    this.writeByte(v);
  }

  writeTag(fieldNumber: number, wireType: number): void {
    this.writeVarint((fieldNumber << 3) | wireType);
  }

  /** Wire type 0: varint field. */
  writeVarintField(fieldNumber: number, value: number): void {
    if (value === 0) return; // proto3 omits default values.
    this.writeTag(fieldNumber, 0);
    this.writeVarint(value);
  }

  /** Wire type 2: length-delimited (bytes) field. */
  writeBytesField(fieldNumber: number, value: Uint8Array): void {
    if (value.length === 0) return; // proto3 omits default values.
    this.writeTag(fieldNumber, 2);
    this.writeVarint(value.length);
    this.writeBytes(value);
  }

  /** Wire type 2: length-delimited (string, utf-8) field. */
  writeStringField(fieldNumber: number, value: string): void {
    if (value.length === 0) return;
    this.writeBytesField(fieldNumber, new TextEncoder().encode(value));
  }

  finish(): Uint8Array {
    return new Uint8Array(this.chunks);
  }
}

/** One decoded field: its number, wire type, and raw payload. */
export interface WireField {
  fieldNumber: number;
  wireType: number;
  varint?: number;
  bytes?: Uint8Array;
}

/**
 * Decodes a flat protobuf message into its fields, keyed by field number.
 * Only wire types 0 (varint) and 2 (length-delimited) are supported --
 * every message this client needs (relay.proto's admission and Frame
 * messages) uses only those two.
 */
export function decodeFields(buf: Uint8Array): Map<number, WireField[]> {
  const fields = new Map<number, WireField[]>();
  let pos = 0;

  function readVarint(): number {
    let result = 0;
    let shift = 1;
    for (;;) {
      if (pos >= buf.length) throw new Error('proto: truncated varint');
      const b = buf[pos++];
      result += (b & 0x7f) * shift;
      if ((b & 0x80) === 0) return result;
      shift *= 128;
    }
  }

  while (pos < buf.length) {
    const tag = readVarint();
    const fieldNumber = tag >>> 3;
    const wireType = tag & 7;
    let field: WireField;
    if (wireType === 0) {
      field = { fieldNumber, wireType, varint: readVarint() };
    } else if (wireType === 2) {
      const len = readVarint();
      if (pos + len > buf.length) throw new Error('proto: truncated length-delimited field');
      field = { fieldNumber, wireType, bytes: buf.slice(pos, pos + len) };
      pos += len;
    } else {
      throw new Error(`proto: unsupported wire type ${wireType} for field ${fieldNumber}`);
    }
    const existing = fields.get(fieldNumber);
    if (existing) existing.push(field);
    else fields.set(fieldNumber, [field]);
  }
  return fields;
}

export function getBytes(fields: Map<number, WireField[]>, fieldNumber: number): Uint8Array {
  return fields.get(fieldNumber)?.[0]?.bytes ?? new Uint8Array(0);
}

export function getString(fields: Map<number, WireField[]>, fieldNumber: number): string {
  const bytes = fields.get(fieldNumber)?.[0]?.bytes;
  return bytes ? new TextDecoder().decode(bytes) : '';
}

export function getVarint(fields: Map<number, WireField[]>, fieldNumber: number): number {
  return fields.get(fieldNumber)?.[0]?.varint ?? 0;
}
