on run {input}
	set Project to input as string
	
	set oldDelimiters to AppleScript's text item delimiters
	set AppleScript's text item delimiters to {"
", " "}
	set Project to text items of Project
	set AppleScript's text item delimiters to {" "}
	set Project to Project as string
	set AppleScript's text item delimiters to oldDelimiters
	
	set Project to do shell script "echo " & quoted form of Project & " | tr '[:upper:]' '[:lower:]'"
	
	tell application "Ghostty"
		activate
	end tell
	
	delay 0.5
	
	set cmd1 to "brew update && brew upgrade"
	set cmd2 to "z " & Project
	# zellij removed
	set cmd4 to "say 'We are ready to work'"
	
	sendCmd(cmd1)
	delay 0.2
	sendCmd(cmd2)
	delay 0.2
	sendCmd(cmd3)
	delay 0.2
	sendCmd(cmd4)
end run

on sendCmd(theCmd)
	tell application "System Events"
		tell process "Ghostty"
			keystroke "t" using {command down}
			delay 0.3
			keystroke theCmd
			key code 36
			delay 0.2
		end tell
	end tell
end sendCmd