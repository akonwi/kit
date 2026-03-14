import type { ComposerState } from "../state/app-state";

export type ComposerDockProps = {
  composer: ComposerState;
  cwd: string;
  sessionName: string | undefined;
};

export function ComposerDock(props: ComposerDockProps) {
  return (
    <box flexShrink={0}>
      <box
        width="100%"
        border
        borderColor="white"
        paddingLeft={1}
        paddingRight={1}
        paddingBottom={1}
        flexDirection="column"
        gap={0}
      >
        <textarea
          height={props.composer.height}
          initialValue={props.composer.initialValue}
          placeholder={props.composer.placeholder}
          placeholderColor="#666666"
          backgroundColor="#1b1b1b"
          focusedBackgroundColor="#1b1b1b"
          textColor="#f2f2f2"
          focusedTextColor="#f2f2f2"
          cursorColor="#ffffff"
          wrapMode="word"
          focused
        />
      </box>
      <text position="absolute" bottom={0} left={2} fg="#8f8f8f">
        {props.sessionName || "Unnamed"}
      </text>
      <text position="absolute" bottom={0} right={2} fg="#8f8f8f">
        {props.cwd}
      </text>
    </box>
  );
}
