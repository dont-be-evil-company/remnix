Output ctrlr.tape
Require remnix
Require zsh

Set Shell "zsh"
Set FontSize 12
Set Width 800
Set Height 600

Type "echo 'Hello from remnix!'"
Enter
Sleep 1s
Hide
Type "remnix"
Enter
Wait+Screen /.*\[ ALL \].*/
Show
Sleep 1s
Type "echo "
Sleep 2s
Type "foo"
Sleep 4s
